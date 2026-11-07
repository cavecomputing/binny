package server

import (
	"net/http"
	"slices"
	"strings"

	"github.com/cavecomputing/binny/internal/db"
)

const (
	searchLimit   = 2000
	maxSearchTags = 50 // each is a join, and SQLite joins at most 64 tables
)

// listTags is every tag on something in data/files/, with how many items have it, most used first.
func (s *Server) listTags(w http.ResponseWriter, r *http.Request) error {
	counts, err := s.db.TagCounts()
	if err != nil {
		return err
	}
	return reply(w, http.StatusOK, map[string][]db.TagCount{"tags": counts})
}

// tagItems changes the tags of files and folders: {paths, add, remove, rename: [[old, new], ...]}.
// It returns each item's tags afterwards.
func (s *Server) tagItems(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Paths, Add, Remove []string
		Rename             [][]string
	}
	if err := decode(r, &body); err != nil {
		return err
	}
	items, err := s.itemsArg(body.Paths, true)
	if err != nil {
		return err
	}
	add, err := tagsArg(body.Add)
	if err != nil {
		return err
	}
	remove, err := tagsArg(body.Remove)
	if err != nil {
		return err
	}
	var renames [][2]string
	for _, pair := range body.Rename {
		if len(pair) != 2 {
			return fail(http.StatusBadRequest, "Expected renames as [old, new] pairs")
		}
		old, err := tagArg(pair[0])
		if err != nil {
			return err
		}
		new, err := tagArg(pair[1])
		if err != nil {
			return err
		}
		renames = append(renames, [2]string{old, new})
	}
	paths := make([]string, len(items))
	for i, it := range items {
		paths[i] = it.path
	}
	var tags map[string][]string
	err = s.db.Update(func(tx *db.Tx) error {
		if err := tx.ChangeTags(paths, add, remove, renames); err != nil {
			return err
		}
		tags, err = tx.Tags(paths)
		return err
	})
	if err != nil {
		return err
	}
	return reply(w, http.StatusOK, map[string]any{"tags": tags})
}

// renameTag renames a tag on every item, trashed ones included.
func (s *Server) renameTag(w http.ResponseWriter, r *http.Request) error {
	var body struct{ Old, New string }
	if err := decode(r, &body); err != nil {
		return err
	}
	old, err := tagArg(body.Old)
	if err != nil {
		return err
	}
	new, err := tagArg(body.New)
	if err != nil {
		return err
	}
	var count int
	var found bool
	err = s.db.Update(func(tx *db.Tx) (err error) {
		count, found, err = tx.RenameTag(old, new)
		return err
	})
	if err != nil {
		return err
	}
	if !found {
		return fail(http.StatusNotFound, `No tag named "%s"`, old)
	}
	return reply(w, http.StatusOK, map[string]any{"tag": new, "items": count})
}

// deleteTag takes a tag off every item, trashed ones included.
func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) error {
	var body struct{ Tag string }
	if err := decode(r, &body); err != nil {
		return err
	}
	tag, err := tagArg(body.Tag)
	if err != nil {
		return err
	}
	var count int
	var found bool
	err = s.db.Update(func(tx *db.Tx) (err error) {
		count, found, err = tx.DeleteTag(tag)
		return err
	})
	if err != nil {
		return err
	}
	if !found {
		return fail(http.StatusNotFound, `No tag named "%s"`, tag)
	}
	return reply(w, http.StatusOK, map[string]int{"items": count})
}

// search is files and folders anywhere that have every ?tag=, none of the ?not= tags, and ?name=
// in their name.
func (s *Server) search(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()
	include, exclude := searchTags(query["tag"]), searchTags(query["not"])
	name := db.Words(query.Get("name"))
	if len(include) == 0 && len(exclude) == 0 && name == "" {
		return reply(w, http.StatusOK, map[string]any{"items": []db.Item{}, "truncated": false})
	}
	if len(include) > maxSearchTags {
		return fail(http.StatusBadRequest, "Search for at most %d tags at a time", maxSearchTags)
	}
	items, truncated, err := s.db.Search(include, exclude, name, searchLimit)
	if err != nil {
		return err
	}
	return reply(w, http.StatusOK, map[string]any{"items": orEmpty(items), "truncated": truncated})
}

// searchTags is tags from a search, trimmed and lowercased, without repeats.
func searchTags(values []string) []string {
	var tags []string
	for _, value := range values {
		if tag := strings.ToLower(strings.TrimSpace(value)); tag != "" && !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	return tags
}

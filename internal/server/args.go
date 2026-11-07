package server

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"slices"

	"github.com/cavecomputing/binny/internal/db"
	"github.com/cavecomputing/binny/internal/storage"
)

// The checks on what the client sent. Each fails with the message the page shows, so handlers
// can stay on the happy path.

const maxItems = 10_000

// item is a file or folder the client named.
type item struct {
	path string
	info fs.FileInfo
}

// decode reads the request's JSON object into body. No body at all leaves body as it is.
func decode(r *http.Request, body any) error {
	if err := json.NewDecoder(r.Body).Decode(body); err != nil && !errors.Is(err, io.EOF) {
		return fail(http.StatusBadRequest, "Expected a JSON object")
	}
	return nil
}

func cleaned(path string) (string, error) {
	rel, err := storage.CleanPath(path)
	if err != nil {
		return "", fail(http.StatusBadRequest, "%s", err)
	}
	return rel, nil
}

// folderArg is an existing folder the client named; "" is the top folder.
func (s *Server) folderArg(path string) (string, error) {
	rel, err := cleaned(path)
	if err == nil && !s.disk.IsFolder(rel) {
		err = fail(http.StatusNotFound, "That folder no longer exists")
	}
	return rel, err
}

// itemArg is an existing file or folder the client named, other than the top folder.
func (s *Server) itemArg(path string) (item, error) {
	rel, err := cleaned(path)
	if err != nil {
		return item{}, err
	}
	if rel == "" {
		return item{}, fail(http.StatusBadRequest, "Pick a file or folder")
	}
	info, ok := s.disk.Plain(rel)
	if !ok {
		return item{}, fail(http.StatusNotFound, `"%s" no longer exists`, storage.NameOf(rel))
	}
	return item{rel, info}, nil
}

// itemsArg is the files and folders the client named, without repeats. Unless keepNested, an item
// inside another listed folder is left out: moving, trashing or zipping the folder takes it along.
// (A search can list both.)
func (s *Server) itemsArg(paths []string, keepNested bool) ([]item, error) {
	if len(paths) == 0 {
		return nil, fail(http.StatusBadRequest, "Pick at least one file or folder")
	}
	if len(paths) > maxItems {
		return nil, fail(http.StatusBadRequest, "Pick at most %d items at a time", maxItems)
	}
	var items []item
	listed := map[string]bool{}
	for _, path := range paths {
		it, err := s.itemArg(path)
		if err != nil {
			return nil, err
		}
		if !listed[it.path] {
			listed[it.path] = true
			items = append(items, it)
		}
	}
	if !keepNested {
		items = slices.DeleteFunc(items, func(it item) bool { return insideAny(it.path, listed) })
	}
	return items, nil
}

func insideAny(path string, folders map[string]bool) bool {
	for parent := storage.ParentOf(path); parent != ""; parent = storage.ParentOf(parent) {
		if folders[parent] {
			return true
		}
	}
	return false
}

func nameArg(name string) (string, error) {
	name, err := storage.CheckName(name)
	if err != nil {
		return "", fail(http.StatusBadRequest, "%s", err)
	}
	return name, nil
}

func tagArg(tag string) (string, error) {
	tag, err := db.CleanTag(tag)
	if err != nil {
		return "", fail(http.StatusBadRequest, "%s", err)
	}
	return tag, nil
}

// tagsArg is a list of tags from the client, cleaned, without repeats.
func tagsArg(values []string) ([]string, error) {
	tags := []string{}
	for _, value := range values {
		tag, err := tagArg(value)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

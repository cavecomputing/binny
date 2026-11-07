package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cavecomputing/binny/internal/storage"
)

const maxTagLength = 100

// CleanTag trims and lowercases a tag from the client. Whitespace and commas separate tags when
// typed, ">" renames one, and a leading "-", "+" or "@" is an operator in the tag syntax (see
// static/js/tags.js), so none of them can be in a tag.
func CleanTag(tag string) (string, error) {
	tag = strings.ToLower(strings.TrimSpace(tag))
	switch {
	case tag == "":
		return "", errors.New("A tag can't be empty")
	case utf8.RuneCountInString(tag) > maxTagLength:
		return "", fmt.Errorf("A tag can be at most %d characters", maxTagLength)
	case strings.ContainsFunc(tag, func(r rune) bool { return unicode.IsSpace(r) || r == ',' || r == '>' || r < 32 || r == 0x7f }):
		return "", fmt.Errorf(`"%s" can't be a tag: no spaces, commas or ">"`, tag)
	case strings.ContainsAny(tag[:1], "-+@"):
		return "", fmt.Errorf(`A tag can't start with "%s"`, tag[:1])
	}
	return tag, nil
}

// TagCount is a tag and how many items have it.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

func jsonList(values []string) string {
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

// Tags is each of paths' tags, sorted.
func (c conn) Tags(paths []string) (map[string][]string, error) {
	found := make(map[string][]string, len(paths))
	for _, path := range paths {
		found[path] = []string{}
	}
	rows, err := c.q.Query(`SELECT path, tag FROM tags WHERE path IN (SELECT value FROM json_each(?)) ORDER BY tag`, jsonList(paths))
	_, err = collect(rows, err, func(rows *sql.Rows) (struct{}, error) {
		var path, tag string
		err := rows.Scan(&path, &tag)
		found[path] = append(found[path], tag)
		return struct{}{}, err
	})
	return found, err
}

// TagCounts is every tag on something in data/files/ with how many items have it, most used first.
func (c conn) TagCounts() ([]TagCount, error) {
	rows, err := c.q.Query(`SELECT tag, COUNT(*) AS count FROM tags JOIN entries USING (path)
		GROUP BY tag ORDER BY count DESC, tag`)
	counts, err := collect(rows, err, func(rows *sql.Rows) (TagCount, error) {
		var count TagCount
		return count, rows.Scan(&count.Tag, &count.Count)
	})
	if counts == nil {
		counts = []TagCount{}
	}
	return counts, err
}

// MoveTags carries the tags of old and everything below it over to new, after the move on disk.
// Tags already at new are stale.
func (c conn) MoveTags(old, new string) error {
	if err := c.DropTags(new); err != nil {
		return err
	}
	_, err := c.q.Exec(`UPDATE tags SET path = ? || substr(path, ?) WHERE `+inSubtree, append([]any{new, utf8.RuneCountInString(old) + 1}, subtree(old)...)...)
	return err
}

// DropTags forgets the tags of path and everything below it.
func (c conn) DropTags(path string) error {
	_, err := c.q.Exec(`DELETE FROM tags WHERE `+inSubtree, subtree(path)...)
	return err
}

// AddTags tags each of paths with each of tags.
func (c conn) AddTags(paths, tags []string) error {
	_, err := c.q.Exec(`INSERT OR IGNORE INTO tags (path, tag)
		SELECT item.value, tag.value FROM json_each(?) AS item, json_each(?) AS tag`, jsonList(paths), jsonList(tags))
	return err
}

// ChangeTags renames, removes and then adds tags on paths. A rename only touches the items that
// have the old tag.
func (c conn) ChangeTags(paths, add, remove []string, renames [][2]string) error {
	onItems := `path IN (SELECT value FROM json_each(?))`
	list := jsonList(paths)
	for _, rename := range renames {
		old, new := rename[0], rename[1]
		if _, err := c.q.Exec(`INSERT OR IGNORE INTO tags (path, tag) SELECT path, ? FROM tags WHERE tag = ? AND `+onItems, new, old, list); err != nil {
			return err
		}
		if new != old {
			if _, err := c.q.Exec(`DELETE FROM tags WHERE tag = ? AND `+onItems, old, list); err != nil {
				return err
			}
		}
	}
	if _, err := c.q.Exec(`DELETE FROM tags WHERE tag IN (SELECT value FROM json_each(?)) AND `+onItems, jsonList(remove), list); err != nil {
		return err
	}
	return c.AddTags(paths, add)
}

// RenameTag renames a tag on every item, trashed ones included; an item that has both keeps one.
// It returns how many items in data/files/ had it, and false when nothing has it.
func (c conn) RenameTag(old, new string) (int, bool, error) {
	count, found, err := c.tagged(old)
	if err != nil || !found || new == old {
		return count, found, err
	}
	if _, err := c.q.Exec(`INSERT OR IGNORE INTO tags (path, tag) SELECT path, ? FROM tags WHERE tag = ?`, new, old); err != nil {
		return 0, false, err
	}
	_, err = c.q.Exec(`DELETE FROM tags WHERE tag = ?`, old)
	return count, true, err
}

// DeleteTag takes a tag off every item, trashed ones included, like RenameTag.
func (c conn) DeleteTag(tag string) (int, bool, error) {
	count, found, err := c.tagged(tag)
	if err != nil || !found {
		return count, found, err
	}
	_, err = c.q.Exec(`DELETE FROM tags WHERE tag = ?`, tag)
	return count, true, err
}

// tagged is how many indexed items have tag, and whether anything at all has it.
func (c conn) tagged(tag string) (count int, found bool, err error) {
	err = c.q.QueryRow(`SELECT EXISTS(SELECT 1 FROM tags WHERE tag = ?),
		(SELECT COUNT(*) FROM tags JOIN entries USING (path) WHERE tag = ?)`, tag, tag).Scan(&found, &count)
	return count, found, err
}

// Search finds files and folders anywhere that have every one of include, none of exclude, and
// name in their name, compared as Words. Folders come first, then by path, at most limit of
// them; truncated says there were more.
func (c conn) Search(include, exclude []string, name string, limit int) (items []Item, truncated bool, err error) {
	// Joins on the included tags let SQLite start from the tag index; the rest filter what's left.
	var query strings.Builder
	query.WriteString(`SELECT entries.path FROM entries`)
	var args []any
	for i, tag := range include {
		fmt.Fprintf(&query, ` JOIN tags AS t%d ON t%d.path = entries.path AND t%d.tag = ?`, i, i, i)
		args = append(args, tag)
	}
	var where []string
	for _, tag := range exclude {
		where = append(where, `NOT EXISTS (SELECT 1 FROM tags WHERE tags.path = entries.path AND tags.tag = ?)`)
		args = append(args, tag)
	}
	if where != nil {
		query.WriteString(` WHERE ` + strings.Join(where, ` AND `))
	}
	rows, err := c.q.Query(query.String()+` ORDER BY entries.is_dir DESC, entries.path`, args...)
	if err != nil {
		return nil, false, err
	}
	var paths []string
	for rows.Next() && len(paths) <= limit {
		var path string
		if err := rows.Scan(&path); err != nil {
			rows.Close()
			return nil, false, err
		}
		if strings.Contains(Words(storage.NameOf(path)), name) {
			paths = append(paths, path)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	truncated = len(paths) > limit
	paths = paths[:min(len(paths), limit)]
	items, err = c.items(`SELECT path, is_dir, size, mtime FROM entries WHERE path IN (SELECT value FROM json_each(?))
		ORDER BY is_dir DESC, path`, jsonList(paths))
	return items, truncated, err
}

// Words is text as a name search compares it: lowercase, with runs of spaces, "_" and "-" as one
// space.
func Words(text string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return unicode.IsSpace(r) || r == '_' || r == '-'
	}), " ")
}

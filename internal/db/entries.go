package db

import (
	"database/sql"
	"errors"
	"io/fs"
	"log/slog"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/cavecomputing/binny/internal/storage"
)

// A partial file nothing has written to for this long was cut short by a restart, or its folder
// moved while it was written; scanning its folder deletes it.
const stalePartial = 24 * time.Hour

// Item is a file or folder as the explorer draws it.
type Item struct {
	Path  string   `json:"path"`
	Name  string   `json:"name"`
	IsDir bool     `json:"is_dir"`
	Size  int64    `json:"size"` // a folder's counts everything below it
	Mtime float64  `json:"mtime"`
	Kind  string   `json:"kind"`
	Items *int     `json:"items,omitempty"` // a folder's, directly inside it
	Tags  []string `json:"tags"`
}

type entry struct {
	isDir bool
	size  int64
	mtime float64
}

// entryOf is the row for info, false for anything but an ordinary file or folder.
func entryOf(info fs.FileInfo) (entry, bool) {
	t := info.ModTime()
	mtime := float64(t.Unix()) + float64(t.Nanosecond())*1e-9
	switch {
	case info.IsDir():
		return entry{true, 0, mtime}, true
	case info.Mode().IsRegular():
		return entry{false, info.Size(), mtime}, true
	}
	return entry{}, false
}

// Refresh brings folder's rows in line with the disk, then those of every folder below it whose
// modified time moved, so things added or removed outside the app show up as far down as the
// change went. A file edited in place doesn't move its folder's time; IndexTree on start catches
// that. One short transaction per folder.
func (db *DB) Refresh(folder string) error {
	return db.walk(folder, false)
}

// IndexTree syncs folder and every folder below it, changed or not.
func (db *DB) IndexTree(folder string) error {
	return db.walk(folder, true)
}

func (db *DB) walk(folder string, everything bool) error {
	pending := []string{folder}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		err := db.Update(func(tx *Tx) error {
			changed, err := tx.sync(current)
			if everything && err == nil {
				rows, err := tx.q.Query(`SELECT path FROM entries WHERE parent = ? AND is_dir`, current)
				changed, err = collect(rows, err, scanString)
			}
			pending = append(pending, changed...)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// sync brings the rows directly inside folder in line with the disk. It returns the subfolders
// that are new to the index or whose modified time moved.
func (tx *Tx) sync(folder string) ([]string, error) {
	onDisk, err := tx.scan(folder)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		if folder == "" {
			return nil, nil
		}
		return nil, tx.Forget(folder)
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.q.Query(`SELECT path, is_dir, size, mtime FROM entries WHERE parent = ?`, folder)
	indexed, err := collect(rows, err, func(rows *sql.Rows) (pathEntry, error) {
		var row pathEntry
		return row, rows.Scan(&row.path, &row.isDir, &row.size, &row.mtime)
	})
	if err != nil {
		return nil, err
	}
	for _, row := range indexed {
		if _, ok := onDisk[row.path]; !ok {
			if err := tx.Forget(row.path); err != nil {
				return nil, err
			}
		}
	}
	old := map[string]entry{}
	for _, row := range indexed {
		old[row.path] = row.entry
	}
	var changed []string
	for path, now := range onDisk {
		was, known := old[path]
		if known && was == now {
			continue
		}
		if known && was.isDir != now.isDir {
			if err := tx.Forget(path); err != nil {
				return nil, err
			}
		}
		if err := tx.put(path, now); err != nil {
			return nil, err
		}
		if now.isDir {
			changed = append(changed, path)
		}
	}
	return changed, nil
}

type pathEntry struct {
	path string
	entry
}

// scan reads the visible entries directly inside folder. Hidden ones (.trash, partial files) and
// symlinks are left out, and so are names that aren't valid UTF-8, which JSON can't carry.
func (tx *Tx) scan(folder string) (map[string]entry, error) {
	children, err := tx.disk.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	found := map[string]entry{}
	for _, child := range children {
		name, path := child.Name(), storage.Child(folder, child.Name())
		info, err := child.Info()
		if err != nil || !utf8.ValidString(name) {
			continue
		}
		if name[0] == '.' {
			if storage.IsPartial(name) && time.Since(info.ModTime()) > stalePartial {
				if err := tx.disk.Remove(path); err == nil {
					slog.Info("Deleted a partial file left behind", "path", path)
				}
			}
			continue
		}
		if row, ok := entryOf(info); ok {
			found[path] = row
		}
	}
	return found, nil
}

func (c conn) put(path string, row entry) error {
	_, err := c.q.Exec(`INSERT OR REPLACE INTO entries (path, parent, is_dir, size, mtime) VALUES (?, ?, ?, ?, ?)`,
		path, storage.ParentOf(path), row.isDir, row.size, row.mtime)
	return err
}

// Record indexes one file or folder just added through the app, without rescanning its folder.
func (c conn) Record(path string) error {
	info, err := c.disk.Lstat(path)
	if err != nil {
		return err
	}
	if row, ok := entryOf(info); ok {
		return c.put(path, row)
	}
	return nil
}

// Forget drops the rows of path and everything below it.
func (c conn) Forget(path string) error {
	_, err := c.q.Exec(`DELETE FROM entries WHERE `+inSubtree, subtree(path)...)
	return err
}

// MoveRows re-keys the rows of old and everything below it to new, after the move on disk. Rows
// already at new are stale.
func (c conn) MoveRows(old, new string) error {
	if err := c.Forget(new); err != nil {
		return err
	}
	start := len(old) + 1
	_, err := c.q.Exec(`UPDATE entries
		SET parent = CASE WHEN path = ? THEN ? ELSE ? || substr(parent, ?) END,
		    path = ? || substr(path, ?)
		WHERE `+inSubtree,
		append([]any{old, storage.ParentOf(new), new, start, new, start}, subtree(old)...)...)
	return err
}

// FolderTotals is how many items are directly inside an indexed folder ("" is the top folder),
// and how many bytes are in everything below it.
func (c conn) FolderTotals(path string) (count int, size int64, err error) {
	if err := c.q.QueryRow(`SELECT COUNT(*) FROM entries WHERE parent = ?`, path).Scan(&count); err != nil {
		return 0, 0, err
	}
	if path == "" {
		err = c.q.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM entries`).Scan(&size)
	} else {
		err = c.q.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM entries WHERE path > ? AND path < ?`, path+"/", path+"0").Scan(&size)
	}
	return count, size, err
}

// List is what's in folder, as the index has it.
func (c conn) List(folder string) ([]Item, error) {
	return c.items(`SELECT path, is_dir, size, mtime FROM entries WHERE parent = ?`, folder)
}

// items runs a query for entry rows (path, is_dir, size, mtime) and makes them Items, with their
// folder totals and tags.
func (c conn) items(query string, args ...any) ([]Item, error) {
	rows, err := c.q.Query(query, args...)
	items, err := collect(rows, err, func(rows *sql.Rows) (Item, error) {
		var item Item
		err := rows.Scan(&item.Path, &item.IsDir, &item.Size, &item.Mtime)
		item.Name = storage.NameOf(item.Path)
		item.Kind = storage.KindOf(item.Name, item.IsDir)
		return item, err
	})
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(items))
	for i := range items {
		if items[i].IsDir {
			count, size, err := c.FolderTotals(items[i].Path)
			if err != nil {
				return nil, err
			}
			items[i].Items, items[i].Size = &count, size
		}
		paths[i] = items[i].Path
	}
	tags, err := c.Tags(paths)
	for i := range items {
		items[i].Tags = tags[items[i].Path]
	}
	return items, err
}

func scanString(rows *sql.Rows) (string, error) {
	var s string
	return s, rows.Scan(&s)
}

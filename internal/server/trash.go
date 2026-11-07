package server

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cavecomputing/binny/internal/db"
	"github.com/cavecomputing/binny/internal/storage"
)

// The trash is data/files/.trash/. A trashed item keeps its name behind the time it was trashed,
// "<time_ns>_<name>", and a trash record remembers where it came from. Its tags wait under
// ".trash/<that name>". Anything in .trash/ counts as trash, including things put there by hand;
// those restore to the top folder.
const trashDir = ".trash"

type trashed struct {
	Name      string  `json:"name"`
	Original  string  `json:"original"`
	IsDir     bool    `json:"is_dir"`
	Size      int64   `json:"size"`
	TrashedAt float64 `json:"trashed_at"`
	Kind      string  `json:"kind"`
}

// trashEntries is the plain files and folders in the trash.
func (s *Server) trashEntries() ([]fs.DirEntry, error) {
	entries, err := s.disk.ReadDir(trashDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return slices.DeleteFunc(entries, func(entry fs.DirEntry) bool {
		return entry.Name()[0] == '.' || !(entry.IsDir() || entry.Type().IsRegular())
	}), err
}

// splitTrashName is when an item was trashed and its original name, from "<time_ns>_<name>"; a
// name not like that wasn't trashed through the app.
func splitTrashName(name string) (trashedAt float64, original string) {
	stamp, original, found := strings.Cut(name, "_")
	ns, err := strconv.ParseUint(stamp, 10, 64)
	if !found || err != nil || original == "" {
		return 0, name
	}
	return float64(ns) / 1e9, original
}

// namesArg is the names of things in the trash the client picked, without repeats.
func (s *Server) namesArg(names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, fail(http.StatusBadRequest, "Pick at least one item in the trash")
	}
	var picked []string
	for _, name := range names {
		if name == "" || strings.ContainsAny(name, "/\x00") || name[0] == '.' {
			return nil, fail(http.StatusBadRequest, "Invalid name")
		}
		if _, ok := s.disk.Plain(trashDir + "/" + name); !ok {
			return nil, fail(http.StatusNotFound, "That is no longer in the trash")
		}
		if !slices.Contains(picked, name) {
			picked = append(picked, name)
		}
	}
	return picked, nil
}

// listTrash is what's in the trash, last trashed first.
func (s *Server) listTrash(w http.ResponseWriter, r *http.Request) error {
	records, err := s.db.TrashRecords()
	if err != nil {
		return err
	}
	entries, err := s.trashEntries()
	if err != nil {
		return err
	}
	items := []trashed{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		trashedAt, original := splitTrashName(entry.Name())
		size := info.Size()
		if entry.IsDir() {
			size = 0
		}
		if record, ok := records[entry.Name()]; ok {
			original, size = record.Original, record.Size
		}
		if trashedAt == 0 {
			trashedAt = float64(info.ModTime().UnixNano()) / 1e9
		}
		items = append(items, trashed{entry.Name(), original, entry.IsDir(), size, trashedAt, storage.KindOf(storage.NameOf(original), entry.IsDir())})
	}
	slices.SortStableFunc(items, func(a, b trashed) int { return cmp.Compare(b.TrashedAt, a.TrashedAt) })
	return reply(w, http.StatusOK, map[string][]trashed{"items": items})
}

// trash moves files and folders to the trash, and returns their names there, which undo restores.
func (s *Server) trash(w http.ResponseWriter, r *http.Request) error {
	var body struct{ Paths []string }
	if err := decode(r, &body); err != nil {
		return err
	}
	items, err := s.itemsArg(body.Paths, false)
	if err != nil {
		return err
	}
	if err := s.disk.Mkdir(trashDir, 0o777); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	names := []string{}
	for _, it := range items {
		size := it.info.Size()
		if it.info.IsDir() {
			if err := s.db.Refresh(it.path); err != nil { // so its size counts what's in it now
				return err
			}
			if _, size, err = s.db.FolderTotals(it.path); err != nil {
				return err
			}
		}
		// The record keeps the whole name; the disk takes at most MaxNameBytes.
		name := storage.Shortened(fmt.Sprintf("%d_%s", time.Now().UnixNano(), storage.NameOf(it.path)), storage.MaxNameBytes)
		err := s.db.Update(func(tx *db.Tx) error {
			if err := s.disk.Rename(it.path, trashDir+"/"+name); err != nil {
				return err
			}
			if err := tx.Forget(it.path); err != nil {
				return err
			}
			if err := tx.MoveTags(it.path, trashDir+"/"+name); err != nil {
				return err
			}
			return tx.AddTrashed(name, it.path, size)
		})
		if err != nil {
			return err
		}
		names = append(names, name)
	}
	return reply(w, http.StatusOK, map[string][]string{"names": names})
}

// restore puts trashed items back where they were. A folder they were in is made again if it's
// gone, and a taken name gets " (1)".
func (s *Server) restore(w http.ResponseWriter, r *http.Request) error {
	var body struct{ Names []string }
	if err := decode(r, &body); err != nil {
		return err
	}
	names, err := s.namesArg(body.Names)
	if err != nil {
		return err
	}
	restored := []string{}
	for _, name := range names {
		var path string
		isDir := s.disk.IsFolder(trashDir + "/" + name)
		err := s.db.Update(func(tx *db.Tx) error {
			original, ok, err := tx.TrashedFrom(name)
			if err != nil {
				return err
			}
			if !ok {
				_, original = splitTrashName(name)
			}
			parent := storage.ParentOf(original)
			created, err := s.disk.MakeFolders(parent)
			if invalid, ok := errors.AsType[storage.InvalidPath](err); ok {
				return fail(http.StatusConflict, `Can't restore "%s": %s`, storage.NameOf(original), invalid)
			} else if err != nil {
				return err
			}
			s.disk.NameLock.Lock()
			path = storage.Child(parent, s.disk.FreeName(parent, storage.NameOf(original), isDir))
			err = s.disk.Rename(trashDir+"/"+name, path)
			s.disk.NameLock.Unlock()
			if err != nil {
				return err
			}
			if err := recordAll(tx, created); err != nil {
				return err
			}
			if err := tx.Record(path); err != nil {
				return err
			}
			if err := tx.MoveTags(trashDir+"/"+name, path); err != nil {
				return err
			}
			return tx.ForgetTrashed(name)
		})
		if err != nil {
			return err
		}
		if isDir {
			if err := s.db.IndexTree(path); err != nil {
				return err
			}
		}
		restored = append(restored, path)
	}
	return reply(w, http.StatusOK, map[string][]string{"restored": restored})
}

func (s *Server) deleteForever(w http.ResponseWriter, r *http.Request) error {
	var body struct{ Names []string }
	if err := decode(r, &body); err != nil {
		return err
	}
	names, err := s.namesArg(body.Names)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := s.disk.RemoveAll(trashDir + "/" + name); err != nil {
			return err
		}
	}
	err = s.db.Update(func(tx *db.Tx) error {
		for _, name := range names {
			if err := tx.ForgetTrashed(name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return reply(w, http.StatusOK, map[string]int{"deleted": len(names)})
}

func (s *Server) emptyTrash(w http.ResponseWriter, r *http.Request) error {
	entries, err := s.trashEntries()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := s.disk.RemoveAll(trashDir + "/" + entry.Name()); err != nil {
			return err
		}
	}
	if err := s.db.Update(func(tx *db.Tx) error { return tx.ForgetAllTrashed() }); err != nil {
		return err
	}
	return reply(w, http.StatusOK, map[string]int{"deleted": len(entries)})
}

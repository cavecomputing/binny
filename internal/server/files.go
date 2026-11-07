package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/cavecomputing/binny/internal/db"
	"github.com/cavecomputing/binny/internal/storage"
)

// list is what's in a folder, synced with the disk first so changes made outside the app show up.
func (s *Server) list(w http.ResponseWriter, r *http.Request) error {
	folder, err := s.folderArg(r.URL.Query().Get("folder"))
	if err != nil {
		return err
	}
	if err := s.db.Refresh(folder); err != nil {
		return err
	}
	items, err := s.db.List(folder)
	if err != nil {
		return err
	}
	return reply(w, http.StatusOK, map[string]any{"folder": folder, "items": orEmpty(items)})
}

// upload streams one file into a folder: PUT the bytes to ?folder=<folder>&path=<name>.
//
// path may start with subfolders ("trip/day 1/a.jpg") when a dropped folder is uploaded: missing
// ones are made and existing ones used as they are. A taken name gets " (1)", never an overwrite.
func (s *Server) upload(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()
	folder, err := s.folderArg(query.Get("folder"))
	if err != nil {
		return err
	}
	parts := strings.Split(query.Get("path"), "/")
	for i, part := range parts {
		if parts[i], err = nameArg(part); err != nil {
			return err
		}
	}
	name := parts[len(parts)-1]
	folder = strings.Trim(strings.Join(append([]string{folder}, parts[:len(parts)-1]...), "/"), "/")
	created, err := s.disk.MakeFolders(folder)
	if err != nil {
		if invalid, ok := errors.AsType[storage.InvalidPath](err); ok {
			return fail(http.StatusConflict, "%s", invalid)
		}
		return err
	}
	if len(created) > 0 {
		if err := s.db.Update(func(tx *db.Tx) error { return recordAll(tx, created) }); err != nil {
			return err
		}
	}

	partial := storage.Child(folder, storage.PartialName())
	out, err := s.disk.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return err
	}
	defer s.disk.Remove(partial)
	_, err = io.Copy(out, r.Body)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		if storage.Problem(err) == "" {
			return fail(http.StatusBadRequest, "The upload was cut short")
		}
		return err
	}
	s.disk.NameLock.Lock()
	name = s.disk.FreeName(folder, name, false)
	err = s.disk.Rename(partial, storage.Child(folder, name))
	s.disk.NameLock.Unlock()
	if err != nil {
		return err
	}

	path := storage.Child(folder, name)
	if err := s.db.Update(func(tx *db.Tx) error { return recordAll(tx, []string{path}) }); err != nil {
		return err
	}
	return reply(w, http.StatusCreated, map[string]string{"path": path})
}

// recordAll indexes things the app just made, which start without tags.
func recordAll(tx *db.Tx, paths []string) error {
	for _, path := range paths {
		if err := tx.Record(path); err != nil {
			return err
		}
		if err := tx.DropTags(path); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) rename(w http.ResponseWriter, r *http.Request) error {
	var body struct{ Path, Name string }
	if err := decode(r, &body); err != nil {
		return err
	}
	it, err := s.itemArg(body.Path)
	if err != nil {
		return err
	}
	name, err := nameArg(body.Name)
	if err != nil {
		return err
	}
	newPath := storage.Child(storage.ParentOf(it.path), name)
	if newPath != it.path {
		s.disk.NameLock.Lock()
		_, err := s.disk.Lstat(newPath)
		if err == nil {
			s.disk.NameLock.Unlock()
			return fail(http.StatusConflict, `There is already a "%s" here`, name)
		}
		err = s.disk.Rename(it.path, newPath)
		s.disk.NameLock.Unlock()
		if err != nil {
			return err
		}
		if err := s.db.Update(func(tx *db.Tx) error { return moved(tx, it.path, newPath) }); err != nil {
			return err
		}
	}
	return reply(w, http.StatusOK, map[string]string{"path": newPath})
}

// moved re-keys what the index and the tags know about old to new, after the move on disk.
func moved(tx *db.Tx, old, new string) error {
	if err := tx.MoveRows(old, new); err != nil {
		return err
	}
	return tx.MoveTags(old, new)
}

// move moves files and folders into another folder. A taken name gets " (1)", never an overwrite.
func (s *Server) move(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Paths []string
		To    string
	}
	if err := decode(r, &body); err != nil {
		return err
	}
	items, err := s.itemsArg(body.Paths, false)
	if err != nil {
		return err
	}
	dest, err := s.folderArg(body.To)
	if err != nil {
		return err
	}
	for _, it := range items {
		if dest == it.path || strings.HasPrefix(dest, it.path+"/") {
			return fail(http.StatusBadRequest, `"%s" can't go inside itself`, storage.NameOf(it.path))
		}
	}
	movedCount, renamed := 0, 0
	for _, it := range items {
		if storage.ParentOf(it.path) == dest {
			continue
		}
		original := storage.NameOf(it.path)
		s.disk.NameLock.Lock()
		name := s.disk.FreeName(dest, original, it.info.IsDir())
		err := s.disk.Rename(it.path, storage.Child(dest, name))
		s.disk.NameLock.Unlock()
		if err != nil {
			return err
		}
		if err := s.db.Update(func(tx *db.Tx) error { return moved(tx, it.path, storage.Child(dest, name)) }); err != nil {
			return err
		}
		movedCount++
		if name != original {
			renamed++
		}
	}
	return reply(w, http.StatusOK, map[string]int{"moved": movedCount, "renamed": renamed})
}

// orEmpty is items, or an empty list rather than JSON's null.
func orEmpty[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

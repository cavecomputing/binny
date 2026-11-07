package server

import (
	"errors"
	"io/fs"
	"net/http"

	"github.com/cavecomputing/binny/internal/db"
	"github.com/cavecomputing/binny/internal/storage"
)

// folderTree is the sidebar: the subfolders of the top folder and of each ?open=<folder>, how
// full the disk is, and how much is in the trash.
func (s *Server) folderTree(w http.ResponseWriter, r *http.Request) error {
	tree := map[string][]db.Folder{}
	for _, folder := range append([]string{""}, r.URL.Query()["open"]...) {
		folder, err := storage.CleanPath(folder)
		if err != nil {
			continue
		}
		if tree[folder], err = s.db.Subfolders(folder); err != nil {
			return err
		}
	}
	_, filesSize, err := s.db.FolderTotals("")
	if err != nil {
		return err
	}
	total, used, free, err := s.disk.Usage()
	if err != nil {
		return err
	}
	trashed, err := s.trashEntries()
	if err != nil {
		return err
	}
	return reply(w, http.StatusOK, map[string]any{
		"tree":        tree,
		"files_size":  filesSize,
		"disk":        map[string]uint64{"total": total, "used": used, "free": free},
		"trash_items": len(trashed),
	})
}

// allFolders is every folder's path, for picking where a download goes.
func (s *Server) allFolders(w http.ResponseWriter, r *http.Request) error {
	folders, err := s.db.AllFolders()
	if err != nil {
		return err
	}
	return reply(w, http.StatusOK, map[string][]string{"folders": folders})
}

func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) error {
	var body struct{ Parent, Name string }
	if err := decode(r, &body); err != nil {
		return err
	}
	parent, err := s.folderArg(body.Parent)
	if err != nil {
		return err
	}
	name, err := nameArg(body.Name)
	if err != nil {
		return err
	}
	path := storage.Child(parent, name)
	if err := s.disk.Mkdir(path, 0o777); errors.Is(err, fs.ErrExist) {
		return fail(http.StatusConflict, `There is already a "%s" here`, name)
	} else if err != nil {
		return err
	}
	if err := s.db.Update(func(tx *db.Tx) error { return recordAll(tx, []string{path}) }); err != nil {
		return err
	}
	return reply(w, http.StatusCreated, map[string]string{"path": path})
}

package db

import (
	"database/sql"

	"github.com/cavecomputing/binny/internal/storage"
)

// Folder is a folder in the sidebar's tree.
type Folder struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	HasChildren bool   `json:"has_children"`
}

// Subfolders is the folders directly inside folder.
func (c conn) Subfolders(folder string) ([]Folder, error) {
	rows, err := c.q.Query(`SELECT path, EXISTS(SELECT 1 FROM entries AS sub WHERE sub.parent = entries.path AND sub.is_dir)
		FROM entries WHERE parent = ? AND is_dir`, folder)
	folders, err := collect(rows, err, func(rows *sql.Rows) (Folder, error) {
		var f Folder
		err := rows.Scan(&f.Path, &f.HasChildren)
		f.Name = storage.NameOf(f.Path)
		return f, err
	})
	if folders == nil {
		folders = []Folder{}
	}
	return folders, err
}

// AllFolders is every folder's path.
func (c conn) AllFolders() ([]string, error) {
	rows, err := c.q.Query(`SELECT path FROM entries WHERE is_dir`)
	paths, err := collect(rows, err, scanString)
	if paths == nil {
		paths = []string{}
	}
	return paths, err
}

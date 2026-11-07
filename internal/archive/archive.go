// Package archive streams zips while they're written, so a folder of any size downloads without
// a temporary file and without being held in memory.
package archive

import (
	"archive/zip"
	"io"
	"log/slog"

	"github.com/cavecomputing/binny/internal/storage"
)

// Entry is a file or folder on disk and the name it gets in the zip.
type Entry struct {
	Path, Name string
}

// Write writes a zip of entries to w, each folder with every plain file and folder visible below
// it. Files are stored, not compressed: the big ones (photos, video, archives) don't shrink, and
// storing keeps the server's CPU out of the way.
func Write(w io.Writer, disk *storage.Disk, entries []Entry) error {
	zw := zip.NewWriter(w)
	for _, entry := range entries {
		if err := add(zw, disk, entry.Path, entry.Name); err != nil {
			return err
		}
	}
	return zw.Close()
}

func add(zw *zip.Writer, disk *storage.Disk, path, name string) error {
	info, ok := disk.Plain(path)
	if !ok {
		return nil // gone since it was picked, or something a read would block on
	}
	header := &zip.FileHeader{Name: name, Method: zip.Store, Modified: info.ModTime()}
	if info.IsDir() {
		header.Name += "/"
		if _, err := zw.CreateHeader(header); err != nil {
			return err
		}
		children, err := disk.ReadDir(path)
		if err != nil {
			slog.Warn("Left a folder's contents out of a zip", "path", path, "err", err)
			return nil
		}
		for _, child := range children {
			if child.Name()[0] != '.' {
				if err := add(zw, disk, storage.Child(path, child.Name()), name+"/"+child.Name()); err != nil {
					return err
				}
			}
		}
		return nil
	}
	file, err := disk.Open(path)
	if err != nil {
		slog.Warn("Left a file out of a zip", "path", path, "err", err)
		return nil
	}
	defer file.Close()
	dest, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(dest, file)
	return err
}

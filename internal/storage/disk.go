package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	"syscall"
)

// Disk is data/files/, opened as an os.Root so that no path, symlinks included, leads out of it.
// Paths are relative to it, "" being the folder itself.
type Disk struct {
	*os.Root
	// NameLock is held while picking a free name and renaming into it, so two writers never
	// claim the same name.
	NameLock sync.Mutex
}

func OpenDisk(dir string) (*Disk, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Disk{Root: root}, nil
}

func at(rel string) string {
	if rel == "" {
		return "."
	}
	return rel
}

func (d *Disk) Lstat(rel string) (fs.FileInfo, error) { return d.Root.Lstat(at(rel)) }

func (d *Disk) Open(rel string) (*os.File, error) { return d.Root.Open(at(rel)) }

// ReadDir lists a folder, sorted by name.
func (d *Disk) ReadDir(rel string) ([]fs.DirEntry, error) {
	return fs.ReadDir(d.FS(), at(rel))
}

// Plain returns rel's info if it is an ordinary file or folder: not a symlink, a pipe or a
// device, which Binny neither lists nor serves.
func (d *Disk) Plain(rel string) (fs.FileInfo, bool) {
	info, err := d.Lstat(rel)
	return info, err == nil && (info.IsDir() || info.Mode().IsRegular())
}

// IsFolder reports whether rel is a folder, not a symlink to one.
func (d *Disk) IsFolder(rel string) bool {
	info, err := d.Lstat(rel)
	return err == nil && info.IsDir()
}

// FreeName is the first of Numbered(name) not taken in folder. Hold NameLock.
func (d *Disk) FreeName(folder, name string, isDir bool) string {
	for candidate := range Numbered(name, isDir) {
		if _, err := d.Lstat(Child(folder, candidate)); err != nil {
			return candidate
		}
	}
	panic("unreachable")
}

// MakeFolders creates the missing folders of rel, top first, and returns the ones it created. It
// refuses with InvalidPath when a file stands where a folder should be.
func (d *Disk) MakeFolders(rel string) ([]string, error) {
	var created []string
	if rel == "" {
		return nil, nil
	}
	current := ""
	for part := range strings.SplitSeq(rel, "/") {
		current = Child(current, part)
		err := d.Mkdir(current, 0o777)
		if err == nil {
			created = append(created, current)
			continue
		}
		if !errors.Is(err, fs.ErrExist) {
			return created, err
		}
		if !d.IsFolder(current) {
			return created, InvalidPath(fmt.Sprintf(`"%s" is a file, not a folder`, part))
		}
	}
	return created, nil
}

// Usage is the size of the filesystem holding the files, how much of it is used and how much is
// free to write, in bytes.
func (d *Disk) Usage() (total, used, free uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(d.Name(), &st); err != nil {
		return 0, 0, 0, err
	}
	size := uint64(st.Bsize)
	return st.Blocks * size, (st.Blocks - st.Bfree) * size, st.Bavail * size, nil
}

// Problem is what the system said went wrong in err, such as "Permission denied", or "" when err
// doesn't carry one.
func Problem(err error) string {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return ""
	}
	text := errno.Error()
	return strings.ToUpper(text[:1]) + text[1:]
}

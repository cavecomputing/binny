//go:build !windows

package storage

import "syscall"

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

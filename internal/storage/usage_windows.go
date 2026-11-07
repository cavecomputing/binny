package storage

import (
	"syscall"
	"unsafe"
)

var getDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// Usage is the size of the filesystem holding the files, how much of it is used and how much is
// free to write, in bytes.
func (d *Disk) Usage() (total, used, free uint64, err error) {
	dir, err := syscall.UTF16PtrFromString(d.Name())
	if err != nil {
		return 0, 0, 0, err
	}
	var allFree uint64
	ok, _, err := getDiskFreeSpaceEx.Call(uintptr(unsafe.Pointer(dir)),
		uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&allFree)))
	if ok == 0 {
		return 0, 0, 0, err
	}
	return total, total - allFree, free, nil
}

//go:build !windows

package storage

import (
	"syscall"
	"testing"
)

// makePipe makes a named pipe at path.
func makePipe(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o666); err != nil {
		t.Fatal(err)
	}
}

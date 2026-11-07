package server

import "testing"

// makePipe makes nothing: Windows has no named pipes in a folder. A test's checks that a pipe is
// refused then see a missing file, which is refused the same way.
func makePipe(*testing.T, string) {}

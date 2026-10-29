//go:build !windows

package spec

import (
	"errors"
	"os"
	"syscall"
)

// openNoFollow opens path read-only without following a final symlink and
// without blocking on a FIFO.
func openNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}

// isSymlinkRefusal reports the error O_NOFOLLOW gives for a symlink.
func isSymlinkRefusal(err error) bool {
	return errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EMLINK)
}

//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// openRegular opens path for reading without following a symlink and
// without blocking on a FIFO or device.
func openRegular(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}

// openRegularIn opens name under root for reading without blocking on a
// FIFO or device. The open never leaves root: a symlink inside it may be
// followed, but one (or a swapped parent) that points out of it fails the
// open, so callers Lstat before and fstat after.
func openRegularIn(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}

// renameIn renames the entry n under root to dest. n's parent is opened
// through root, so it never resolves outside it (a parent swapped for a
// symlink out of root fails the open), and the rename is relative to that
// open folder: renameat doesn't follow n itself, so a swapped n moves only
// as the link it is. src (n's plain path) is unused here.
func renameIn(root *os.Root, src, n, dest string) error {
	p, err := root.Open(filepath.Dir(n))
	if err != nil {
		return err
	}
	defer p.Close()
	d, err := os.Open(filepath.Dir(dest))
	if err != nil {
		return err
	}
	defer d.Close()
	return unix.Renameat(int(p.Fd()), filepath.Base(n), int(d.Fd()), filepath.Base(dest))
}

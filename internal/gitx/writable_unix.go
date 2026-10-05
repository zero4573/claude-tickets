//go:build !windows

package gitx

import "golang.org/x/sys/unix"

// Writable is false on a read-only mount too, not only by permissions.
func Writable(dir string) bool { return unix.Access(dir, unix.W_OK) == nil }

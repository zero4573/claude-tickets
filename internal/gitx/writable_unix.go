//go:build !windows

package gitx

import "golang.org/x/sys/unix"

// Writable reports whether ct may write in dir (read-only mounts count).
func Writable(dir string) bool { return unix.Access(dir, unix.W_OK) == nil }

//go:build !windows

package gitx

import "golang.org/x/sys/unix"

// Writable is false on a read-only mount too, not only by permissions.
func Writable(dir string) bool { return unix.Access(dir, unix.W_OK) == nil }

// searchable is access(p, X_OK), as git's is_git_directory tests a git
// dir's objects and refs.
func searchable(p string) bool { return unix.Access(p, unix.X_OK) == nil }

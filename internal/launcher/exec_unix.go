//go:build !windows

package launcher

import "syscall"

// execReplace replaces this process with path (so the terminal belongs to
// it); it returns only on failure.
func execReplace(path string, argv, env []string) error { return syscall.Exec(path, argv, env) }

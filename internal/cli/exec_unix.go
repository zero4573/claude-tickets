//go:build !windows

package cli

import "syscall"

// execReplace replaces ct with another program.
func execReplace(path string, argv, env []string) error { return syscall.Exec(path, argv, env) }

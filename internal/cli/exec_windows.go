//go:build windows

package cli

import "errors"

// execReplace isn't possible on Windows: the caller runs the program instead.
func execReplace(path string, argv, env []string) error { return errors.New("no exec on windows") }

//go:build windows

package cli

import "os"

func openRegular(path string) (*os.File, error) { return os.Open(path) }

func openRegularIn(root *os.Root, name string) (*os.File, error) { return root.Open(name) }

// renameIn renames src (the entry n under root) to dest. Windows has no
// renameat: moveTree's checkInside is the only guard here, so the
// parent-swap guarantee of the unix renameIn does not hold on Windows.
func renameIn(_ *os.Root, src, _, dest string) error { return os.Rename(src, dest) }

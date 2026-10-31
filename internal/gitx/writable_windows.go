//go:build windows

package gitx

import "os"

func Writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".tix-writable-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// searchable is git for Windows' access(p, X_OK), which drops X_OK: p
// exists.
func searchable(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

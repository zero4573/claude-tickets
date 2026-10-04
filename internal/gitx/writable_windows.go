//go:build windows

package gitx

import "os"

// Writable reports whether ct may write in dir.
func Writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".ct-writable-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

//go:build windows

package gitx

import "os"

func Writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".ct-writable-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

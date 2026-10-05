// Package lock serializes writers of a shared file with an exclusive lock
// on a sibling lock file (flock on Unix, LockFileEx on Windows).
package lock

import "os"

func With(path string, fn func() error) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := lockFile(f); err != nil {
		return err
	}
	defer unlockFile(f)
	return fn()
}

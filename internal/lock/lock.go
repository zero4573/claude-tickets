// Package lock serializes writers of a shared file with an exclusive lock
// on a sibling lock file (flock on Unix, LockFileEx on Windows).
package lock

import (
	"context"
	"os"
	"time"
)

// WithContext is With, but waiting for a held lock stops when ctx is done
// (it returns ctx.Err()). It tries without blocking first; if the lock is
// held it calls waiting (when non-nil) once, then polls.
func WithContext(ctx context.Context, path string, waiting func(), fn func() error) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	got, err := tryLockFile(f)
	if err != nil {
		return err
	}
	if !got {
		if waiting != nil {
			waiting()
		}
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for !got {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
			}
			if got, err = tryLockFile(f); err != nil {
				return err
			}
		}
	}
	defer unlockFile(f)
	return fn()
}

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

//go:build !linux && !darwin && !windows

package proc

// identity can't be told here: callers fall back to other signals.
func identity(int) (string, error) { return "", ErrUnsupported }

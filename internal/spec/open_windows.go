//go:build windows

package spec

import "os"

// openNoFollow opens path read-only; the caller checks the descriptor's
// mode (Windows has no O_NOFOLLOW, and the walk already skipped symlinks).
func openNoFollow(path string) (*os.File, error) {
	return os.Open(path)
}

func isSymlinkRefusal(error) bool { return false }

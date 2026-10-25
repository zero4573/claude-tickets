package embed

import (
	"os"
	"path/filepath"
	"strings"
)

// libSearchDirs lists the directories to look for native dependencies in.
func libSearchDirs() []string {
	var dirs []string
	for _, list := range []string{os.Getenv("CLAUDE_TICKETS_LIB_PATH"), DefaultLibPath} {
		for _, d := range strings.Split(list, string(os.PathListSeparator)) {
			if d != "" {
				dirs = append(dirs, d)
			}
		}
	}
	return dirs
}

// findLib returns the first dir/name that exists.
func findLib(dirs []string, name string) string {
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

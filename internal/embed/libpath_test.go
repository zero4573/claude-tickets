package embed

import (
	"path/filepath"
	"testing"
)

func TestLibSearchDirsDropsRelative(t *testing.T) {
	abs := t.TempDir()
	t.Setenv("CLAUDE_TICKETS_LIB_PATH", "rel:.:"+abs)
	var got []string
	for _, d := range libSearchDirs() {
		if !filepath.IsAbs(d) {
			t.Errorf("relative dir kept: %q", d)
		}
		got = append(got, d)
	}
	if len(got) == 0 || got[0] != abs {
		t.Fatalf("dirs: %v", got)
	}
}

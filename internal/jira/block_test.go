package jira

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteBlockKeepsTheNote(t *testing.T) {
	p := parts{head: "## Source (Jira)\n"}
	for name, tc := range map[string]struct{ in, want string }{
		"replaced": {
			"---\n---\n# X\n\n<!-- source:start -->\nold\n<!-- source:end -->\n\n## Tasks\n- [ ] a\n",
			"---\n---\n# X\n\n<!-- source:start -->\n## Source (Jira)\n<!-- source:end -->\n\n## Tasks\n- [ ] a\n"},
		"no end marker": {
			"---\n---\n# X\n\n<!-- source:start -->\nold\n\n## Tasks\n- [ ] a\n",
			"---\n---\n# X\n\n<!-- source:start -->\n## Source (Jira)\n<!-- source:end -->\nold\n\n## Tasks\n- [ ] a\n"},
		"end before start": {
			"# X\n<!-- source:end -->\n<!-- source:start -->\n## Tasks\n",
			"# X\n<!-- source:end -->\n<!-- source:start -->\n## Source (Jira)\n<!-- source:end -->\n## Tasks\n"},
		"no block": {
			"# X\n\n## Tasks\n",
			"# X\n\n<!-- source:start -->\n## Source (Jira)\n<!-- source:end -->\n\n## Tasks\n"},
	} {
		f := filepath.Join(t.TempDir(), "n.md")
		os.WriteFile(f, []byte(tc.in), 0o644)
		if err := writeBlock(f, p); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(f)
		if string(got) != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", name, got, tc.want)
		}
		if !strings.Contains(string(got), "## Tasks") {
			t.Errorf("%s: lost the note's own sections", name)
		}
	}
}

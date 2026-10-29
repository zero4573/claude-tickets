package jira

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zero4573/claude-tickets/internal/note"
)

func writeNote(t *testing.T, vault, source, id, body string) string {
	t.Helper()
	p := filepath.Join(vault, "tickets", source, id, id+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// A stub filed under a source epic (ct ticket stub) is not in the epic's
// children yet: the epic's refresh keeps its covered-by, but clears that of a
// synced note that left the epic.
func TestWriteChildrenKeepsStubCoveredBy(t *testing.T) {
	v := t.TempDir()
	epic := writeNote(t, v, "jira", "PROJ-4", "---\ntitle: PROJ-4\nsource: jira\ncovers: []\ntags: [ticket]\n---\n# PROJ-4\n")
	stub := writeNote(t, v, "jira", "PROJ-30", "---\ntitle: PROJ-30\nsource: jira\nsource-id: PROJ-30\ncovered-by: \"[[PROJ-4]]\"\ntags: [ticket]\n---\n# PROJ-30\n")
	left := writeNote(t, v, "jira", "PROJ-31", "---\ntitle: PROJ-31\nsource: jira\nsource-url: https://x/browse/PROJ-31\nsource-updated: 2026-01-01T00:00:00\ncovered-by: \"[[PROJ-4]]\"\ntags: [ticket]\n---\n# PROJ-31\n")
	s := &Sync{Vault: v, Source: "jira", Children: []Child{{Epic: "PROJ-4", Key: "PROJ-5", Mine: true, Cat: "new"}}}
	if err := s.writeChildren("PROJ-4", epic, &parts{}); err != nil {
		t.Fatal(err)
	}
	if got := note.Get(stub, "covered-by"); got != `"[[PROJ-4]]"` && got != "[[PROJ-4]]" {
		t.Errorf("stub covered-by = %q", got)
	}
	if got := strings.Trim(note.Get(left, "covered-by"), `"`); got != "" {
		t.Errorf("a note that left the epic kept covered-by = %q", got)
	}
	if c := note.ListField(epic, "covers"); strings.Join(c, ",") != "[[PROJ-5]]" {
		t.Errorf("covers = %v", c)
	}
}

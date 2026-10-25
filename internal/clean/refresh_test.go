package clean

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zero4573/claude-tickets/internal/vindex"
)

// W4 S05: an item that rewrote a note and then failed (here: .trash is a file,
// so the move fails after the links were rewritten) still refreshes the index.
func TestRunRefreshesAfterFailedItem(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "stub")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	o := setup(t)
	ticket(t, o.Vault, "PROJ-1", "status: closed", jira+"PROJ-1")
	put(t, o.Vault, "notes/n.md", "# N\n\nSee [[PROJ-1]] for details.\n")
	if _, err := vindex.Update(o.Vault, vindex.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	put(t, o.Vault, ".trash", "not a directory")
	env := Env{Out: io.Discard, RemoveWorkspace: func(string) error { return nil }}
	res := Run(o, Plan(o), env)
	if !res.Failed || res.Cleaned != 0 {
		t.Fatalf("item should have failed: %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(o.Vault, "notes", "n.md")); !strings.Contains(string(b), "atlassian") {
		t.Fatalf("link not rewritten, test setup is wrong: %s", b)
	}
	ix, err := vindex.Load(o.Vault)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ix.Chunks {
		if strings.Contains(c.Text, "atlassian") {
			return
		}
	}
	t.Fatal("the index was not refreshed after the failed item")
}

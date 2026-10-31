package clean

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/zero4573/tix/internal/vindex"
)

// W4 S05: an item that rewrote a note and then failed (here: .trash is a file,
// so the move fails after the links were rewritten) still refreshes the index.
func TestRunRefreshesAfterFailedItem(t *testing.T) {
	t.Setenv("TIX_EMBEDDER", "stub")
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

// recordRefresh swaps refreshIndex for a recorder of its calls' paths (a nil
// entry: a whole-vault call).
const saved = "saved: 2026-10-01T10:00:00"

func recordRefresh(t *testing.T) *[][]string {
	t.Helper()
	var calls [][]string
	old := refreshIndex
	refreshIndex = func(_ string, paths ...string) { calls = append(calls, paths) }
	t.Cleanup(func() { refreshIndex = old })
	return &calls
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

func runQuiet(o Options, items []Item) Result {
	return Run(o, items, Env{Out: io.Discard, RemoveWorkspace: func(string) error { return nil }})
}

func wantOneCall(t *testing.T, calls *[][]string, want []string) {
	t.Helper()
	if len(*calls) != 1 {
		t.Fatalf("refresh calls = %v, want one with %v", *calls, want)
	}
	if got := sorted((*calls)[0]); !reflect.DeepEqual(got, sorted(want)) {
		t.Fatalf("refreshed %v, want %v", got, sorted(want))
	}
}

func TestRunRefreshesExactPathsArchive(t *testing.T) {
	calls := recordRefresh(t)
	o := setup(t)
	ticket(t, o.Vault, "MAN-1", "status: closed", "source: manual", saved)
	put(t, o.Vault, "tickets/manual/MAN-1/log.md", "# log\n")
	put(t, o.Vault, "notes/p.md", "see [[tickets/manual/MAN-1/log]]\n")
	put(t, o.Vault, "notes/other.md", "nothing\n")
	if res := runQuiet(o, Plan(o)); res.Cleaned != 1 || res.Failed {
		t.Fatalf("%+v", res)
	}
	wantOneCall(t, calls, []string{"tickets/manual/MAN-1/MAN-1.md", "archive/tickets/manual/MAN-1/MAN-1.md",
		"tickets/manual/MAN-1/log.md", "archive/tickets/manual/MAN-1/log.md", "notes/p.md"})
}

func TestRunRefreshesExactPathsTrashAndPurge(t *testing.T) {
	for _, purge := range []bool{false, true} {
		calls := recordRefresh(t)
		o := setup(t)
		o.Purge = purge
		ticket(t, o.Vault, "PROJ-1", "status: closed", saved, jira+"PROJ-1")
		put(t, o.Vault, "tickets/jira/PROJ-1/extra.md", "# extra\n")
		put(t, o.Vault, "notes/n.md", "See [[PROJ-1]] for details.\n")
		put(t, o.Vault, "notes/keep.md", "unrelated\n")
		if res := runQuiet(o, Plan(o)); res.Cleaned != 1 || res.Failed {
			t.Fatalf("purge=%v: %+v", purge, res)
		}
		wantOneCall(t, calls, []string{"notes/n.md", "tickets/jira/PROJ-1/PROJ-1.md", "tickets/jira/PROJ-1/extra.md"})
	}
}

func TestRunRefreshesKeptFilesAndFollowUp(t *testing.T) {
	calls := recordRefresh(t)
	o := setup(t)
	ticket(t, o.Vault, "PROJ-1", "status: closed", saved, jira+"PROJ-1")
	put(t, o.Vault, "tickets/jira/PROJ-1/doc.md", "# doc\n")
	put(t, o.Vault, "notes/n.md", "See [[PROJ-1]] and [[tickets/jira/PROJ-1/doc]].\n")
	// a frontmatter form tix clean can't rewrite: it leaves a follow-up note
	put(t, o.Vault, "notes/fm.md", "---\nrelated: [[PROJ-1]] and more\n---\n")
	res := runQuiet(o, Plan(o))
	if res.Cleaned != 1 || res.FollowUp == "" {
		t.Fatalf("setup: %+v", res)
	}
	fu, _ := filepath.Rel(o.Vault, res.FollowUp)
	wantOneCall(t, calls, []string{"notes/n.md", "tickets/jira/PROJ-1/PROJ-1.md", "tickets/jira/PROJ-1/doc.md",
		"archive/tickets/jira/PROJ-1/doc.md", filepath.ToSlash(fu)})
}

func TestRunFallsBackToWholeVault(t *testing.T) {
	// an item that failed after it wrote
	calls := recordRefresh(t)
	o := setup(t)
	ticket(t, o.Vault, "PROJ-1", "status: closed", jira+"PROJ-1")
	put(t, o.Vault, "notes/n.md", "See [[PROJ-1]].\n")
	put(t, o.Vault, ".trash", "not a directory")
	if res := runQuiet(o, Plan(o)); !res.Failed {
		t.Fatal("item should have failed")
	}
	if !reflect.DeepEqual(*calls, [][]string{nil}) {
		t.Fatalf("failed item: refresh calls = %v, want one whole-vault call", *calls)
	}

	// a /tix:save ran (here it fails, nothing is cleaned)
	calls = recordRefresh(t)
	o = setup(t)
	ticket(t, o.Vault, "MAN-1", "status: closed", "source: manual")
	items := Plan(o)
	items[0].Save, items[0].SaveIn = true, "MAN-1"
	env := Env{Out: io.Discard, RemoveWorkspace: func(string) error { return nil },
		Save: func(string) (string, error) { return "log", errors.New("boom") }}
	if res := Run(o, items, env); res.Cleaned != 0 {
		t.Fatalf("%+v", res)
	}
	if len(*calls) != 1 || (*calls)[0] != nil {
		t.Fatalf("save ran: refresh calls = %v, want one whole-vault call", *calls)
	}
}

func TestRunNothingCleanedDoesNotRefresh(t *testing.T) {
	calls := recordRefresh(t)
	o := setup(t)
	ticket(t, o.Vault, "MAN-1", "status: in-progress", "source: manual")
	runQuiet(o, Plan(o))
	runQuiet(setup(t), nil)
	if len(*calls) != 0 {
		t.Fatalf("refresh calls = %v, want none", *calls)
	}
}

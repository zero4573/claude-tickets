package ticketconv

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zero4573/claude-tickets/internal/mcp/mcptest"
	"github.com/zero4573/claude-tickets/internal/vaultlock"
)

const filedNote = "---\ntitle: MAN-1\nsummary: s\nsource: manual\nsource-id: MAN-1\nconvert-source: jira\nconvert-key: KEY-1\nconvert-url: https://x/browse/KEY-1\ntags: [ticket, manual]\n---\n# MAN-1: s\n\nsee MAN-1\n"

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// newVault is a vault with a jira source and the manual ticket MAN-1 (which
// has a status file beside its note) already filed as KEY-1.
func newVault(t *testing.T) (vault, work string) {
	t.Helper()
	vault, work = t.TempDir(), t.TempDir()
	write(t, filepath.Join(vault, ".obsidian", "app.json"), "{}")
	write(t, filepath.Join(vault, "tickets", ".sources.json"), `{"sources":{"jira":{"mcp":"atlassian","project":"KEY"}}}`)
	write(t, filepath.Join(vault, "tickets", "manual", "MAN-1", "MAN-1.md"), filedNote)
	write(t, filepath.Join(vault, "tickets", "manual", "MAN-1", "MAN-1-status.md"), "status of MAN-1\n")
	return vault, work
}

func opts(v, w string) Options {
	return Options{Vault: v, WorkRoot: w, ID: "MAN-1", Source: "jira", Out: &bytes.Buffer{}}
}

func assertDone(t *testing.T, v string) {
	t.Helper()
	dir := filepath.Join(v, "tickets", "jira", "KEY-1")
	note := read(t, filepath.Join(dir, "KEY-1.md"))
	for _, want := range []string{"source: jira\n", "source-id: KEY-1\n"} {
		if !strings.Contains(note, want) {
			t.Errorf("note lacks %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "converted-from") || strings.Contains(note, "convert-key") {
		t.Errorf("records left in the note:\n%s", note)
	}
	if got := read(t, filepath.Join(dir, "KEY-1-status.md")); got != "status of KEY-1\n" {
		t.Errorf("status = %q", got)
	}
	if _, err := os.Stat(filepath.Join(v, "tickets", "manual", "MAN-1")); err == nil {
		t.Error("old folder left")
	}
}

func TestResumeAfterMoveBeforeFlip(t *testing.T) {
	v := t.TempDir()
	write(t, filepath.Join(v, ".obsidian", "app.json"), "{}")
	write(t, filepath.Join(v, "tickets", ".sources.json"), `{"sources":{"jira":{"mcp":"x"}}}`)
	write(t, filepath.Join(v, "tickets", "jira", "KEY-1", "KEY-1.md"), "---\nsource: manual\nconvert-source: jira\nconvert-key: KEY-1\nconvert-url: u\nsummary: s\n---\n# x\n")
	err := Run(Options{Vault: v, WorkRoot: t.TempDir(), ID: "MAN-1", Source: "jira"})
	if err != nil {
		t.Errorf("resume refused: %v", err)
	}
	if n := read(t, filepath.Join(v, "tickets", "jira", "KEY-1", "KEY-1.md")); !strings.Contains(n, "source: jira") {
		t.Errorf("not flipped:\n%s", n)
	}
}

func TestResumeSplitFolder(t *testing.T) {
	v, w := newVault(t)
	// the status file moved, the note didn't yet
	write(t, filepath.Join(v, "tickets", "jira", "KEY-1", "KEY-1-status.md"), "status of MAN-1\n")
	if err := os.Remove(filepath.Join(v, "tickets", "manual", "MAN-1", "MAN-1-status.md")); err != nil {
		t.Fatal(err)
	}
	if err := Run(opts(v, w)); err != nil {
		t.Fatal(err)
	}
	assertDone(t, v)
}

func TestResumeAfterNoteMovedFilesLeft(t *testing.T) {
	v, w := newVault(t)
	// the note moved (still manual) while a file is left in the old folder
	write(t, filepath.Join(v, "tickets", "jira", "KEY-1", "KEY-1.md"), filedNote)
	if err := os.Remove(filepath.Join(v, "tickets", "manual", "MAN-1", "MAN-1.md")); err != nil {
		t.Fatal(err)
	}
	if err := Run(opts(v, w)); err != nil {
		t.Fatal(err)
	}
	assertDone(t, v)
}

func TestMoveFailureThenRerun(t *testing.T) {
	v, w := newVault(t)
	orig := renameTree
	renameTree = func(vault, src, dst string, rename func(string) string, last string) error {
		return errors.New("injected")
	}
	err := Run(opts(v, w))
	renameTree = orig
	var pe *PartialError
	if !errors.As(err, &pe) || pe.Key != "KEY-1" {
		t.Fatalf("err = %v, want a PartialError for KEY-1", err)
	}
	// the note is still findable and carries the resume record
	if n := read(t, filepath.Join(v, "tickets", "manual", "MAN-1", "MAN-1.md")); !strings.Contains(n, "converted-from: MAN-1") {
		t.Errorf("no resume record:\n%s", n)
	}
	if err := Run(opts(v, w)); err != nil {
		t.Fatal(err)
	}
	assertDone(t, v)
}

func TestMovePartwayThenRerun(t *testing.T) {
	v, w := newVault(t)
	// the move stopped after the status file: the note moves last, so it is still here
	write(t, filepath.Join(v, "tickets", "jira", "KEY-1", "KEY-1-status.md"), "status of MAN-1\n")
	if err := os.Remove(filepath.Join(v, "tickets", "manual", "MAN-1", "MAN-1-status.md")); err != nil {
		t.Fatal(err)
	}
	n := read(t, filepath.Join(v, "tickets", "manual", "MAN-1", "MAN-1.md"))
	write(t, filepath.Join(v, "tickets", "manual", "MAN-1", "MAN-1.md"), SetFields(n, "converted-from", "MAN-1"))
	if err := Run(opts(v, w)); err != nil {
		t.Fatal(err)
	}
	assertDone(t, v)
}

func TestFlipWriteFailureThenRerun(t *testing.T) {
	v, w := newVault(t)
	orig := writeFile
	writeFile = func(p string, b []byte, m os.FileMode) error {
		if filepath.Base(p) == "KEY-1.md" {
			return errors.New("injected")
		}
		return orig(p, b, m)
	}
	err := Run(opts(v, w))
	writeFile = orig
	var pe *PartialError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a PartialError", err)
	}
	if err := Run(opts(v, w)); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	assertDone(t, v)
}

func TestDroppingConvertedFromWarns(t *testing.T) {
	v, w := newVault(t)
	orig := writeFile
	writeFile = func(p string, b []byte, m os.FileMode) error {
		if filepath.Base(p) == "KEY-1.md" && !strings.Contains(string(b), "converted-from") {
			return errors.New("injected")
		}
		return orig(p, b, m)
	}
	o := opts(v, w)
	err := Run(o)
	writeFile = orig
	if err != nil {
		t.Fatal(err)
	}
	if out := o.Out.(*bytes.Buffer).String(); !strings.Contains(out, "warning: couldn't drop 'converted-from'") {
		t.Errorf("no warning:\n%s", out)
	}
}

func TestHeldVaultLockRefuses(t *testing.T) {
	v, w := newVault(t)
	if err := vaultlock.Acquire(v, "save"); err != nil {
		t.Fatal(err)
	}
	origWait := vaultlock.Wait
	vaultlock.Wait = time.Millisecond
	err := Run(opts(v, w))
	vaultlock.Wait = origWait
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("err = %v, want a lock refusal", err)
	}
	if _, err := os.Stat(filepath.Join(v, "tickets", "manual", "MAN-1", "MAN-1.md")); err != nil {
		t.Error("changed despite the lock")
	}
	// and it releases its own
	if err := vaultlock.Release(v, "save"); err != nil {
		t.Fatal(err)
	}
	if err := Run(opts(v, w)); err != nil {
		t.Fatal(err)
	}
	if h, _ := vaultlock.Status(v); strings.Contains(h, "held") && strings.Contains(h, "convert") {
		t.Errorf("lock left: %s", h)
	}
}

func TestSymlinkedWorkRoot(t *testing.T) {
	v, _ := newVault(t)
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "work")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	repo := filepath.Join(real, "MAN-1", "acme-repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	// workspace.json spells the path through the real folder, WorkRoot the link
	write(t, filepath.Join(real, "MAN-1", "workspace.json"), `{"id":"MAN-1","repos":[{"slug":"acme-repo","path":"`+repo+`"}]}`)
	if err := Run(opts(v, link)); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(real, "KEY-1", "workspace.json"))
	if !strings.Contains(got, filepath.Join(link, "KEY-1", "acme-repo")) || strings.Contains(got, "MAN-1") {
		t.Errorf("workspace.json not repointed:\n%s", got)
	}
}

func TestMissingRepoPathIsAnError(t *testing.T) {
	v, w := newVault(t)
	write(t, filepath.Join(w, "MAN-1", "workspace.json"), `{"id":"MAN-1","repos":[{"slug":"gone","path":"/elsewhere/MAN-1/gone"}]}`)
	err := Run(opts(v, w))
	var pe *PartialError
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "gone") {
		t.Fatalf("err = %v, want a PartialError naming the path", err)
	}
}

func TestRunningSessionInNewWorkspace(t *testing.T) {
	v, w := newVault(t)
	write(t, filepath.Join(w, "KEY-1", "workspace.json"), `{"id":"KEY-1","repos":[]}`)
	write(t, filepath.Join(w, "KEY-1", ".agent-state"), `{"state":"working"}`)
	err := Run(opts(v, w))
	var pe *PartialError
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "session is running") {
		t.Fatalf("err = %v, want a refusal for the running session", err)
	}
}

func connect(t *testing.T, f mcptest.Fixture) *mcptest.Server {
	t.Helper()
	s := mcptest.New(f)
	t.Cleanup(s.Close)
	cfg := filepath.Join(t.TempDir(), "mcp.json")
	write(t, cfg, s.Config("atlassian"))
	t.Setenv("CLAUDE_TICKETS_MCP_CONFIG", cfg)
	t.Setenv("CLAUDE_TICKETS_MCP_PREPARE", "")
	return s
}

func fixtureFor() mcptest.Fixture {
	return mcptest.Fixture{
		Me:         "u-me",
		Resources:  []byte(`[{"cloudId":"c1","url":"https://x.atlassian.net/","products":[{"id":"jira"}]}]`),
		Issues:     nil,
		IssueTypes: map[string][]string{"KEY": {"Story", "Bug", "Task", "Epic"}},
	}
}

func unfiled(t *testing.T) (string, string) {
	v, w := newVault(t)
	write(t, filepath.Join(v, "tickets", "manual", "MAN-1", "MAN-1.md"), "---\ntitle: MAN-1\nsummary: s\nticket-type: dev\nsource: manual\nsource-id: MAN-1\ntags: [ticket, manual]\n---\n# MAN-1: s\n")
	return v, w
}

func TestLostCreateResponseWarns(t *testing.T) {
	f := fixtureFor()
	f.DropAfterCreate = true
	s := connect(t, f)
	v, w := unfiled(t)
	err := Run(opts(v, w))
	if err == nil || !strings.Contains(err.Error(), "may exist") || !strings.Contains(err.Error(), "KEY") || strings.Contains(err.Error(), "nothing changed") {
		t.Fatalf("err = %v", err)
	}
	if n := strings.Count(strings.Join(s.Calls, "\n"), "createJiraIssue"); n != 1 {
		t.Errorf("createJiraIssue called %d times", n)
	}
}

func TestInvalidKeyIsNotRecorded(t *testing.T) {
	f := fixtureFor()
	f.CreateKey = "KEY 1\nx"
	connect(t, f)
	v, w := unfiled(t)
	err := Run(opts(v, w))
	var pe *PartialError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a PartialError", err)
	}
	if n := read(t, filepath.Join(v, "tickets", "manual", "MAN-1", "MAN-1.md")); strings.Contains(n, "convert-key") || strings.Contains(n, "x\n") {
		t.Errorf("the bad key was written:\n%s", n)
	}
}

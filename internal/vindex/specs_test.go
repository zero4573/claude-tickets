package vindex

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeSpec(t *testing.T, repo, capName, body string) {
	t.Helper()
	d := filepath.Join(repo, "specs", capName)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "spec.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateSpecs(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "none")
	repo, cache := t.TempDir(), t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	writeSpec(t, repo, "ledger", "# Ledger\n\nThe quokka ledger.\n")
	writeSpec(t, repo, "audit", "# Audit\n\nThe wombat audit.\n")
	if err := os.WriteFile(filepath.Join(repo, "specs", "README.md"), []byte("# not a spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "init")

	root, ok, err := UpdateSpecs(cache, "acme-billing", repo, UpdateOptions{})
	if err != nil || !ok {
		t.Fatalf("UpdateSpecs: %v %v", ok, err)
	}
	if want := SpecIndexRoot(cache, "acme-billing"); root != want {
		t.Fatalf("root %s, want %s", root, want)
	}
	ix, err := LoadSpecs(cache, "acme-billing")
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Notes) != 2 {
		t.Fatalf("notes %+v", ix.Notes)
	}
	n := ix.Notes[0]
	if n.Path != "specs/audit/spec.md" || n.Name != "audit" || n.Type != SpecType || len(n.Projects) != 1 || n.Projects[0] != "acme-billing" {
		t.Fatalf("note %+v", n)
	}

	// Same tree: untouched. An edit on a branch the clone is on, not its
	// default branch, is not read.
	before, _ := os.Stat(filepath.Join(root, ".ct", "index", "meta.json"))
	if _, _, err := UpdateSpecs(cache, "acme-billing", repo, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(filepath.Join(root, ".ct", "index", "meta.json"))
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("index rewritten for an unchanged tree")
	}
	clone := filepath.Join(t.TempDir(), "clone")
	if out, err := exec.Command("git", "clone", "-q", repo, clone).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// ct's git refuses a local-path origin; nothing below fetches
	git(t, clone, "remote", "set-url", "origin", "git@example.invalid:acme/billing.git")
	git(t, clone, "checkout", "-q", "-b", "other")
	writeSpec(t, clone, "ledger", "# Ledger\n\nThe quokka ledgex.\n")
	git(t, clone, "commit", "-q", "-am", "edit")
	if _, _, err := UpdateSpecs(cache, "acme-billing", clone, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	ix, _ = LoadSpecs(cache, "acme-billing")
	for _, c := range ix.Chunks {
		if contains(c.Text, "ledgex") {
			t.Fatalf("branch edit leaked: %q", c.Text)
		}
	}
}

func TestUpdateSpecsSameSizeEdit(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "none")
	repo, cache := t.TempDir(), t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	writeSpec(t, repo, "ledger", "# Ledger\n\nquokka\n")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "init")
	if _, _, err := UpdateSpecs(cache, "r", repo, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, repo, "ledger", "# Ledger\n\nwombat\n")
	git(t, repo, "commit", "-q", "-am", "edit")
	if _, _, err := UpdateSpecs(cache, "r", repo, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	ix, _ := LoadSpecs(cache, "r")
	if len(ix.Chunks) == 0 || ix.Chunks[0].Text == "" || !contains(ix.Chunks[0].Text, "wombat") {
		t.Fatalf("not rebuilt: %+v", ix.Chunks)
	}
}

func TestUpdateSpecsNone(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "none")
	repo, cache := t.TempDir(), t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	if _, ok, err := UpdateSpecs(cache, "r", repo, UpdateOptions{}); err != nil || ok {
		t.Fatalf("%v %v", ok, err)
	}
	if _, err := LoadSpecs(cache, "r"); !errors.Is(err, ErrNoIndex) {
		t.Fatalf("err %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

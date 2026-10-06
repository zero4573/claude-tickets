package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zero4573/claude-tickets/internal/scaffold"
)

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Jane Doe", "-c", "user.email=jane@example.com", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func put(t *testing.T, dir, rel, data string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func history(t *testing.T, file string) map[string][]string {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	h, err := scaffold.ParseHistory(data)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestRun(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	gitT(t, repo, "init", "-q")
	put(t, repo, "tools/vault-scaffold/AGENTS.md", "v1\n")
	put(t, repo, "tools/vault-scaffold/obsidian-types.json", "{}\n")
	put(t, repo, "tools/vault-scaffold/templates/t.md", "t1\n")
	put(t, repo, "README.md", "not shipped\n")
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-q", "-m", "one")
	put(t, repo, "tools/vault-scaffold/AGENTS.md", "v2\n")
	gitT(t, repo, "commit", "-q", "-am", "two")
	if err := os.Mkdir(filepath.Join(repo, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "mv", "tools/vault-scaffold", "assets/vault-scaffold")
	gitT(t, repo, "commit", "-q", "-m", "move")
	put(t, repo, "assets/vault-scaffold/AGENTS.md", "v3\r\n")
	gitT(t, repo, "commit", "-q", "-am", "edit after the move")
	// not committed yet: still listed
	put(t, repo, "assets/vault-scaffold/AGENTS.md", "v4\n")

	out := filepath.Join(repo, "assets", "scaffold-history.json")
	// an existing file's extra hash and path are kept
	extra := scaffold.Hash([]byte("v0\n"))
	put(t, repo, "assets/scaffold-history.json", `{"files": {"AGENTS.md": ["`+extra+`"], "old.md": ["`+extra+`"]}}`)
	if err := run(filepath.Join(repo, "assets"), out); err != nil {
		t.Fatal(err)
	}
	h := history(t, out)
	for _, v := range []string{"v0", "v1", "v2", "v3", "v4"} {
		if !has(h["AGENTS.md"], scaffold.Hash([]byte(v+"\n"))) {
			t.Errorf("AGENTS.md %s missing: %v", v, h["AGENTS.md"])
		}
	}
	if len(h["AGENTS.md"]) != 5 || len(h["templates/t.md"]) != 1 || !has(h["old.md"], extra) {
		t.Errorf("history %v", h)
	}
	for _, rel := range []string{"obsidian-types.json", "README.md", "scaffold-history.json"} {
		if _, ok := h[rel]; ok {
			t.Errorf("%s listed", rel)
		}
	}
	// as the test of the embedded file expects it
	data, _ := os.ReadFile(out)
	if formatted, _ := scaffold.FormatHistory(h); string(formatted) != string(data) {
		t.Error("not written sorted")
	}

	// Again: nothing changes
	if err := run(repo, out); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(out); string(again) != string(data) {
		t.Error("a second run changed the file")
	}

	shallow := filepath.Join(t.TempDir(), "shallow")
	gitT(t, t.TempDir(), "clone", "-q", "--depth", "1", "file://"+filepath.ToSlash(repo), shallow)
	err := run(shallow, filepath.Join(shallow, "h.json"))
	if err == nil || !strings.Contains(err.Error(), "shallow") {
		t.Errorf("shallow clone: %v", err)
	}
}

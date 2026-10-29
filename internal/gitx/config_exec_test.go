package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func plantedRepo(t *testing.T) (repo, marker, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the planted scripts are sh")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	t.Setenv("GIT_SSH_COMMAND", "true")
	dir := t.TempDir()
	repo = filepath.Join(dir, "r")
	git(t, dir, "init", "-q", repo)
	marker = filepath.Join(dir, "marker")
	script = filepath.Join(dir, "plant")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return repo, marker, script
}

func ran(marker string) bool {
	_, err := os.Stat(marker)
	return err == nil
}

func TestSSHCommandAndCredentialHelperOff(t *testing.T) {
	repo, marker, script := plantedRepo(t)
	git(t, repo, "config", "core.sshCommand", script)
	git(t, repo, "config", "credential.helper", "!"+script+" #")

	// Sanity: plain git runs both (without the user's GIT_SSH_COMMAND)
	t.Setenv("GIT_SSH_COMMAND", "")
	os.Unsetenv("GIT_SSH_COMMAND")
	_ = exec.Command("git", "-C", repo, "fetch", "ssh://example.invalid/x").Run()
	if !ran(marker) {
		t.Skip("this git doesn't run core.sshCommand here; nothing to prove")
	}
	_ = os.Remove(marker)
	cred := exec.Command("git", "-C", repo, "credential", "fill")
	cred.Stdin = strings.NewReader("protocol=https\nhost=example.invalid\n\n")
	_ = cred.Run()
	if !ran(marker) {
		t.Fatal("plain git didn't run the planted credential helper; the check below would prove nothing")
	}
	_ = os.Remove(marker)

	_ = Run(repo, "fetch", "ssh://example.invalid/x")
	if ran(marker) {
		t.Fatal("core.sshCommand ran under gitx")
	}
	c := Command("-C", repo, "credential", "fill")
	c.Stdin = strings.NewReader("protocol=https\nhost=example.invalid\n\n")
	_ = c.Run()
	if ran(marker) {
		t.Fatal("a credential helper from the repo's config ran under gitx")
	}

	// The user's own GIT_SSH_COMMAND is kept
	git(t, repo, "config", "--unset", "core.sshCommand")
	git(t, repo, "config", "--unset", "credential.helper")
	t.Setenv("GIT_SSH_COMMAND", script)
	_ = Run(repo, "fetch", "ssh://example.invalid/x")
	if !ran(marker) {
		t.Fatal("the user's GIT_SSH_COMMAND was not used")
	}
}

func TestFilterRefused(t *testing.T) {
	repo, marker, _ := plantedRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("* filter=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	filt := filepath.Join(filepath.Dir(repo), "filt")
	if err := os.WriteFile(filt, []byte("#!/bin/sh\ntouch "+marker+"\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "config", "filter.x.clean", filt)
	git(t, repo, "config", "filter.x.smudge", filt)
	// Sanity: plain git runs the filter
	_ = exec.Command("git", "-C", repo, "add", "f").Run()
	if !ran(marker) {
		t.Skip("this git doesn't run the planted filter; nothing to prove")
	}
	_ = os.Remove(marker)

	for _, args := range [][]string{{"status", "--porcelain"}, {"diff", "--", "f"}, {"checkout", "--", "f"}, {"worktree", "add", filepath.Join(t.TempDir(), "w"), "-b", "b"}} {
		out, err := OutIn(repo, nil, args...)
		if err == nil {
			t.Fatalf("git %v ran in a repo with filter.x.clean: %q", args, out)
		}
		if !strings.Contains(err.Error(), "filter.x.") {
			t.Fatalf("git %v: error %q doesn't name the key", args, err)
		}
		if ran(marker) {
			t.Fatalf("git %v ran the filter", args)
		}
	}
	// Every call is refused, not only those that run filters
	if _, err := Out(repo, "rev-parse", "--git-dir"); err == nil {
		t.Fatal("rev-parse ran in a repo with filter.x.clean")
	}
	if Dirty(repo) != true {
		t.Fatal("a refused status must count as dirty")
	}

	// lfs is allowed
	git(t, repo, "config", "--unset", "filter.x.clean")
	git(t, repo, "config", "--unset", "filter.x.smudge")
	git(t, repo, "config", "filter.lfs.smudge", "git-lfs smudge -- %f")
	git(t, repo, "config", "filter.lfs.process", "git-lfs filter-process")
	if err := Check(repo); err != nil {
		t.Fatalf("lfs refused: %v", err)
	}
}

func TestNoSigningFlags(t *testing.T) {
	found := map[string]bool{}
	f := safeFlags()
	for i, a := range f {
		if a == "-c" && i+1 < len(f) {
			found[f[i+1]] = true
		}
	}
	for _, k := range []string{"credential.helper=", "commit.gpgSign=false", "tag.gpgSign=false"} {
		if !found[k] {
			t.Errorf("safeFlags lacks -c %s", k)
		}
	}
}

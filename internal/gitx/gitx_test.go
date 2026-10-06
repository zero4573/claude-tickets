package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Jane Doe", "GIT_AUTHOR_EMAIL=jane@example.com",
		"GIT_COMMITTER_NAME=Jane Doe", "GIT_COMMITTER_EMAIL=jane@example.com", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// A clone of a bare "remote" with one pushed commit on main, and a
// worktree of it on a ticket branch.
func repo(t *testing.T) (clone, wt string) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	remote, clone, wt := filepath.Join(root, "remote.git"), filepath.Join(root, "clone"), filepath.Join(root, "wt")
	git(t, root, "init", "--quiet", "--bare", "-b", "main", remote)
	git(t, root, "clone", "--quiet", remote, clone)
	git(t, clone, "commit", "--quiet", "--allow-empty", "-m", "first")
	git(t, clone, "push", "--quiet", "origin", "HEAD:main")
	git(t, clone, "worktree", "add", "--quiet", "-b", "feature/PROJ-12", wt)
	return clone, wt
}

func TestUnpushedHead(t *testing.T) {
	_, wt := repo(t)
	if u, err := UnpushedHead(wt); err != nil || u {
		t.Errorf("fresh branch: unpushed = %t, %v", u, err)
	}
	git(t, wt, "commit", "--quiet", "--allow-empty", "-m", "work")
	if u, err := UnpushedHead(wt); err != nil || !u {
		t.Errorf("a commit on no remote: unpushed = %t, %v", u, err)
	}
	git(t, wt, "push", "--quiet", "origin", "HEAD:feature/PROJ-12")
	if u, err := UnpushedHead(wt); err != nil || u {
		t.Errorf("pushed: unpushed = %t, %v", u, err)
	}
	if _, err := UnpushedHead(t.TempDir()); err == nil {
		t.Error("not a repo: no error")
	}
}

func TestDirty(t *testing.T) {
	clone, _ := repo(t)
	if Dirty(clone) {
		t.Error("clean clone is dirty")
	}
	os.WriteFile(filepath.Join(clone, "new"), []byte("x"), 0o644)
	if !Dirty(clone) {
		t.Error("untracked file not dirty")
	}
	if !Dirty(t.TempDir()) {
		t.Error("a failing git must count as dirty")
	}
}

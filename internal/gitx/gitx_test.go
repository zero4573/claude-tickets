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

func commitFile(t *testing.T, dir, name, body, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", name)
	git(t, dir, "commit", "--quiet", "-m", msg)
}

// branch gives the worktree two commits on the ticket branch, and returns
// the clone with origin/main fetched.
func branch(t *testing.T) (clone, wt string) {
	clone, wt = repo(t)
	commitFile(t, wt, "a.txt", "a\n", "a")
	commitFile(t, wt, "b.txt", "b\n", "b")
	return clone, wt
}

// squash puts the branch's net change on main as one commit, from a
// second checkout, as a server's squash merge would, and fetches it.
func squash(t *testing.T, clone string, files map[string]string, then map[string]string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "--quiet", clone+"/../remote.git", other)
	for name, body := range files {
		commitFile(t, other, name, body, "squash "+name)
	}
	git(t, other, "reset", "--quiet", "--soft", "origin/main")
	git(t, other, "commit", "--quiet", "-m", "PROJ-12 squashed")
	for name, body := range then {
		commitFile(t, other, name, body, "later "+name)
	}
	git(t, other, "push", "--quiet", "origin", "HEAD:main")
	git(t, clone, "fetch", "--quiet", "--prune", "origin")
}

func lost(t *testing.T, wt string) bool {
	t.Helper()
	l, err := Lost(wt, "main")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestLostSquashMerged(t *testing.T) {
	clone, wt := branch(t)
	if !lost(t, wt) {
		t.Fatal("unpushed, unmerged: not lost")
	}
	squash(t, clone, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}, nil)
	if lost(t, wt) {
		t.Error("squash-merged: lost")
	}
}

func TestLostSquashMergedThenChanged(t *testing.T) {
	clone, wt := branch(t)
	// main changes a.txt again after the squash: only patch-id can tell
	squash(t, clone, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}, map[string]string{"a.txt": "a2\n"})
	if lost(t, wt) {
		t.Error("squash-merged, then changed: lost")
	}
}

func TestLostRebaseMerged(t *testing.T) {
	clone, wt := branch(t)
	// the same commits re-applied on a moved main (cherry), then a.txt changed
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "--quiet", filepath.Join(filepath.Dir(clone), "remote.git"), other)
	commitFile(t, other, "c.txt", "c\n", "c")
	commitFile(t, other, "a.txt", "a\n", "a")
	commitFile(t, other, "b.txt", "b\n", "b")
	commitFile(t, other, "a.txt", "a3\n", "later")
	git(t, other, "push", "--quiet", "origin", "HEAD:main")
	git(t, clone, "fetch", "--quiet", "origin")
	if lost(t, wt) {
		t.Error("rebase-merged: lost")
	}
}

func TestLostPartlyMerged(t *testing.T) {
	clone, wt := branch(t)
	squash(t, clone, map[string]string{"a.txt": "a\n"}, nil)
	if !lost(t, wt) {
		t.Error("only a.txt merged: not lost")
	}
	// No origin/<base> to compare with: lost
	if l, _ := Lost(wt, "nope"); !l {
		t.Error("no base on origin: not lost")
	}
}

func TestLostSharedCloneStash(t *testing.T) {
	clone, _ := repo(t)
	os.WriteFile(filepath.Join(clone, "s.txt"), []byte("x"), 0o644)
	git(t, clone, "add", "s.txt")
	git(t, clone, "stash", "--quiet")
	if l, err := Lost(clone, "main"); err != nil || !l {
		t.Errorf("stash: %t, %v", l, err)
	}
}

// evilMerge gives dir's ticket branch a commit a, and a merge of a side
// branch (b) whose merge commit adds evil.txt of its own; then base gets
// a and b re-applied (cherry-picked), not the merge.
func evilMerge(t *testing.T, clone, dir string) {
	t.Helper()
	commitFile(t, dir, "a.txt", "a\n", "a")
	git(t, dir, "checkout", "--quiet", "-b", "side", "origin/main")
	commitFile(t, dir, "b.txt", "b\n", "b")
	git(t, dir, "checkout", "--quiet", "feature/PROJ-12")
	git(t, dir, "merge", "--quiet", "--no-ff", "--no-commit", "side")
	if err := os.WriteFile(filepath.Join(dir, "evil.txt"), []byte("only in the merge\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "evil.txt")
	git(t, dir, "commit", "--quiet", "-m", "merge side")
	git(t, dir, "branch", "--quiet", "-D", "side")
	o := otherCheckout(t, clone)
	commitFile(t, o, "a.txt", "a\n", "a")
	commitFile(t, o, "b.txt", "b\n", "b")
	git(t, o, "push", "--quiet", "origin", "HEAD:main")
	git(t, dir, "fetch", "--quiet", "--prune", "origin")
}

// Regression (QA D4): git cherry ignores merge commits, so a merge's own
// content (an evil merge, a conflict resolution) read as merged once the
// ordinary commits were on base.
func TestLostEvilMerge(t *testing.T) {
	clone, wt := repo(t)
	evilMerge(t, clone, wt)
	if !lost(t, wt) {
		t.Error("worktree: a merge commit's own content counted as merged")
	}
}

func TestQAExploreSharedEvil(t *testing.T) {
	clone, _ := repo(t)
	root := filepath.Dir(clone)
	sc := filepath.Join(root, "shared")
	git(t, root, "clone", "--quiet", "--shared", clone, sc)
	git(t, sc, "remote", "set-url", "origin", filepath.Join(root, "remote.git"))
	git(t, sc, "fetch", "--quiet", "origin")
	git(t, sc, "checkout", "--quiet", "-b", "feature/PROJ-12", "origin/main")
	git(t, sc, "branch", "--quiet", "-D", "main")
	evilMerge(t, clone, sc)
	if !lost(t, sc) {
		t.Error("shared clone: a merge commit's own content counted as merged; ct clean would delete it")
	}
}

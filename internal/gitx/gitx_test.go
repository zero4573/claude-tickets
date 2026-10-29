package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Jane Doe", "GIT_AUTHOR_EMAIL=jane@example.com",
		"GIT_COMMITTER_NAME=Jane Doe", "GIT_COMMITTER_EMAIL=jane@example.com")
	if os.Getenv("GIT_CONFIG_GLOBAL") == "" {
		cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL="+os.DevNull)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// remoteURL is the URL tests clone root/<name> by: a local path would fail
// ct's URL rule, so a test global config (the user's, which ct trusts)
// maps it there, over the file transport.
func remoteURL(t *testing.T, root, name string) string {
	t.Helper()
	conf := filepath.Join(root, "gitconfig")
	body := "[url \"" + filepath.ToSlash(root) + "/\"]\n\tinsteadOf = git@example.invalid:\n[protocol \"file\"]\n\tallow = always\n"
	if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", conf)
	return "git@example.invalid:" + name
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
	git(t, root, "clone", "--quiet", remoteURL(t, root, "remote.git"), clone)
	git(t, clone, "commit", "--quiet", "--allow-empty", "-m", "first")
	git(t, clone, "push", "--quiet", "origin", "HEAD:main")
	git(t, clone, "worktree", "add", "--quiet", "-b", "feature/PROJ-12", wt)
	return clone, wt
}

// A detached HEAD's commits are on no branch: Unpushed still counts them.
func TestUnpushedDetachedHead(t *testing.T) {
	clone, _ := repo(t)
	git(t, clone, "fetch", "--quiet", "origin")
	if u, err := Unpushed(clone); err != nil || u {
		t.Errorf("pushed: unpushed = %t, %v", u, err)
	}
	git(t, clone, "checkout", "--quiet", "--detach")
	git(t, clone, "commit", "--quiet", "--allow-empty", "-m", "detached work")
	if u, err := Unpushed(clone); err != nil || !u {
		t.Errorf("a detached commit on no remote: unpushed = %t, %v", u, err)
	}
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
	git(t, clone, "config", "status.showUntrackedFiles", "no")
	if !Dirty(clone) {
		t.Error("status.showUntrackedFiles=no hides an untracked file")
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
	git(t, sc, "remote", "set-url", "origin", "git@example.invalid:remote.git")
	git(t, sc, "fetch", "--quiet", "origin")
	git(t, sc, "checkout", "--quiet", "-b", "feature/PROJ-12", "origin/main")
	git(t, sc, "branch", "--quiet", "-D", "main")
	evilMerge(t, clone, sc)
	if !lost(t, sc) {
		t.Error("shared clone: a merge commit's own content counted as merged; ct clean would delete it")
	}
}

// A hook (core.hooksPath) and an fsmonitor command planted in a repo's
// config must not run under ct's git.
func TestHooksAndFsmonitorOff(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the planted scripts are sh")
	}
	dir := t.TempDir()
	repo := filepath.Join(dir, "r")
	git(t, dir, "init", "-q", repo)
	marker := filepath.Join(dir, "marker")
	hooks := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ntouch " + marker + "\n"
	for _, n := range []string{"pre-commit", "post-checkout", "reference-transaction"} {
		if err := os.WriteFile(filepath.Join(hooks, n), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fsm := filepath.Join(dir, "fsm")
	if err := os.WriteFile(fsm, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	commitFile(t, repo, "f", "x", "x")
	git(t, repo, "branch", "base")
	commitFile(t, repo, "f", "y", "y")
	git(t, repo, "config", "core.hooksPath", hooks)
	git(t, repo, "config", "core.fsmonitor", fsm)
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sanity: the plain git does run them (so the marker proves something)
	plain := exec.Command("git", "-C", repo, "status", "--porcelain")
	_ = plain.Run()
	if _, err := os.Stat(marker); err != nil {
		t.Skip("this git doesn't run the planted fsmonitor; nothing to prove")
	}
	_ = os.Remove(marker)

	// The allowlist refuses the repo outright
	if _, err := Out(repo, "status", "--porcelain"); err == nil || !strings.Contains(err.Error(), `"core.hookspath"`) {
		t.Fatalf("status in a repo with core.hooksPath: %v", err)
	}
	_ = Ok(repo, "add", "f")
	_ = Run(repo, "-c", "user.name=a", "-c", "user.email=a@example.com", "commit", "-q", "-m", "x")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a repo-controlled hook or fsmonitor ran under gitx")
	}
	// The safe flags alone (the second layer) turn them off too
	unchecked(t, repo, "status", "--porcelain")
	unchecked(t, repo, "-c", "user.name=a", "-c", "user.email=a@example.com", "commit", "-q", "-a", "-m", "z")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a repo-controlled hook or fsmonitor ran under the safe flags")
	}

	// A diff.external and a textconv driver (picked by an attributes file)
	// are programs the repo's config names too
	conv := filepath.Join(dir, "conv")
	if err := os.WriteFile(conv, []byte("#!/bin/sh\ntouch "+marker+"\ncat \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "info", "attributes"), []byte("f diff=conv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sanity, each on its own: plain git runs it
	git(t, repo, "config", "diff.conv.textconv", conv)
	for _, args := range [][]string{{"diff", "base", "HEAD"}, {"-c", "diff.external=" + fsm, "diff", "base", "HEAD"}} {
		_ = os.Remove(marker)
		_ = exec.Command("git", append([]string{"-C", repo}, args...)...).Run()
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("plain git %v didn't run the planted program; the check below would prove nothing", args)
		}
	}
	git(t, repo, "config", "diff.external", fsm)
	_ = os.Remove(marker)
	if merged, err := MergedInto(repo, "base"); err == nil || merged {
		t.Fatalf("MergedInto = %t, %v; want a refusal", merged, err)
	}
	// MergedInto's diffs pass --no-ext-diff --no-textconv (second layer)
	unchecked(t, repo, "diff", "--no-ext-diff", "--no-textconv", "base", "HEAD")
	unchecked(t, repo, "log", "--no-ext-diff", "--no-textconv", "-p", "base..HEAD")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a repo-controlled diff.external or textconv ran")
	}
}

// unchecked runs git in dir with ct's safe flags and env but without the
// config check, to prove the second layer on its own.
func unchecked(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append(append(safeFlags(), "-C", dir), args...)...)
	cmd.Env = env()
	_ = cmd.Run()
}

func TestDirOf(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"status"}, ""},
		{[]string{"-C", "a", "log", "-C", "b"}, "a"},
		{[]string{"-c", "x=y", "-C", "a", "-C", "b", "status"}, filepath.Join("a", "b")},
		{[]string{"-C", "a", "-C", string(filepath.Separator) + "b", "status"}, string(filepath.Separator) + "b"},
	} {
		if got := dirOf(c.args); got != c.want {
			t.Errorf("dirOf(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestIndirectionChecks(t *testing.T) {
	t.Cleanup(func() { TrustRoots() })
	clone, wt := repo(t)
	root := filepath.Dir(clone)
	for _, d := range []string{clone, wt, filepath.Join(wt, "sub"), t.TempDir()} {
		if err := checkDir(d); err != nil {
			t.Errorf("checkDir(%s) without roots: %v", d, err)
		}
	}
	TrustRoots(root)
	if err := checkDir(wt); err != nil {
		t.Errorf("a worktree of a clone in the roots: %v", err)
	}
	if _, err := Out(wt, "status", "--porcelain"); err != nil {
		t.Errorf("git in a normal worktree: %v", err)
	}

	// A worktree whose .git file points at a repository made elsewhere
	evil := filepath.Join(t.TempDir(), "evil")
	git(t, filepath.Dir(evil), "init", "-q", evil)
	dotgit := filepath.Join(wt, ".git")
	orig, err := os.ReadFile(dotgit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dotgit, []byte("gitdir: "+filepath.Join(evil, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Out(wt, "status"); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("a .git file pointing out of the roots: %v", err)
	}
	if err := os.WriteFile(dotgit, orig, 0o644); err != nil {
		t.Fatal(err)
	}

	// A worktree's commondir pointing at another repository: refused even
	// without roots, as the git dir isn't one of its worktrees
	TrustRoots()
	cd := filepath.Join(clone, ".git", "worktrees", filepath.Base(wt), "commondir")
	if err := os.WriteFile(cd, []byte(filepath.Join(evil, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Ok(wt, "status") || checkDir(wt) == nil {
		t.Error("a worktree commondir pointing at another repository: not refused")
	}
	if err := os.WriteFile(cd, []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A main clone's .git/commondir
	if err := os.WriteFile(filepath.Join(clone, ".git", "commondir"), []byte(filepath.Join(evil, ".git")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Out(clone, "status"); err == nil || !strings.Contains(err.Error(), "commondir") {
		t.Errorf("a main clone with a commondir: %v", err)
	}
	if err := checkDir(wt); err == nil {
		t.Error("a worktree of a main clone with a commondir: not refused")
	}
}

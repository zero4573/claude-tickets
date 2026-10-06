package gitx

import (
	"path/filepath"
	"testing"
)

// Work that isn't in origin/main must count as lost, however the branch
// and main relate (ct clean must never treat it as merged).

func otherCheckout(t *testing.T, clone string) string {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "--quiet", filepath.Join(filepath.Dir(clone), "remote.git"), other)
	return other
}

func pushMain(t *testing.T, clone, other string) {
	t.Helper()
	git(t, other, "push", "--quiet", "origin", "HEAD:main")
	git(t, clone, "fetch", "--quiet", "--prune", "origin")
}

// onMain puts file on main and brings the ticket branch up to it.
func onMain(t *testing.T, clone, wt, name string) {
	t.Helper()
	o := otherCheckout(t, clone)
	commitFile(t, o, name, name+"\n", name)
	pushMain(t, clone, o)
	git(t, wt, "merge", "--quiet", "--ff-only", "origin/main")
}

func TestLostUnmerged(t *testing.T) {
	t.Run("main changed the same file differently", func(t *testing.T) {
		clone, wt := repo(t)
		commitFile(t, wt, "a.txt", "mine\n", "a")
		o := otherCheckout(t, clone)
		commitFile(t, o, "a.txt", "theirs\n", "a other")
		pushMain(t, clone, o)
		if !lost(t, wt) {
			t.Error("not lost")
		}
	})
	t.Run("a deletion", func(t *testing.T) {
		clone, wt := repo(t)
		onMain(t, clone, wt, "d.txt")
		git(t, wt, "rm", "--quiet", "d.txt")
		git(t, wt, "commit", "--quiet", "-m", "del")
		if !lost(t, wt) {
			t.Error("not lost")
		}
	})
	t.Run("a rename", func(t *testing.T) {
		clone, wt := repo(t)
		onMain(t, clone, wt, "r.txt")
		git(t, wt, "mv", "r.txt", "s.txt")
		git(t, wt, "commit", "--quiet", "-m", "mv")
		if !lost(t, wt) {
			t.Error("not lost")
		}
	})
	t.Run("a file name that is a glob", func(t *testing.T) {
		clone, wt := repo(t)
		onMain(t, clone, wt, "a.txt")
		commitFile(t, wt, "[a].txt", "work\n", "glob name")
		if !lost(t, wt) {
			t.Error("not lost")
		}
	})
	t.Run("a shallow clone", func(t *testing.T) {
		clone, _ := repo(t)
		root := filepath.Dir(clone)
		sh := filepath.Join(root, "shallow")
		git(t, root, "clone", "--quiet", "--depth", "1", "file://"+filepath.Join(root, "remote.git"), sh)
		git(t, sh, "checkout", "--quiet", "-b", "feature/X")
		commitFile(t, sh, "z.txt", "z\n", "z")
		if l, _ := Lost(sh, "main"); !l {
			t.Error("not lost")
		}
	})
}

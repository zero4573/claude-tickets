// Package gitx runs git: captured output for queries, passed-through
// output for the commands the user watches, and the shared clones that
// stand in for worktrees where a main clone's .git isn't writable.
package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Out trims stdout's trailing newline and discards stderr.
func Out(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimRight(string(out), "\n"), err
}

func Ok(dir string, args ...string) bool {
	return exec.Command("git", append([]string{"-C", dir}, args...)...).Run() == nil
}

func Run(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func Timeout(d time.Duration, dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Run()
}

func HasRef(dir, ref string) bool {
	return Ok(dir, "rev-parse", "-q", "--verify", ref)
}

// BaseRef is the ref a ticket branch is compared against: origin/<base>
// if the remote branch is there, else the local <base>.
func BaseRef(dir, base string) string {
	if HasRef(dir, "refs/remotes/origin/"+base) {
		return "origin/" + base
	}
	return base
}

func Dirty(dir string, extra ...string) bool {
	// A git that fails counts as dirty: callers use this to decide whether
	// work would be lost. No optional locks: a status shouldn't write the
	// index (ct clean --dry-run changes nothing)
	out, err := Out(dir, append([]string{"--no-optional-locks", "status", "--porcelain"}, extra...)...)
	return err != nil || out != ""
}

// Unpushed reports whether a clone holds work no remote has: commits on any
// local branch (not only HEAD's), or a stash. An error means "can't tell".
func Unpushed(dir string) (bool, error) {
	n, err := Out(dir, "rev-list", "--count", "--branches", "--not", "--remotes")
	if err != nil {
		return false, err
	}
	stash, err := Out(dir, "stash", "list")
	if err != nil {
		return false, err
	}
	return n != "0" || stash != "", nil
}

// UnpushedHead reports whether HEAD has commits no remote has: what a
// worktree's ticket branch would lose if it were deleted (its stash lives
// in the main clone). An error means "can't tell".
func UnpushedHead(dir string) (bool, error) {
	n, err := Out(dir, "rev-list", "--count", "HEAD", "--not", "--remotes")
	if err != nil {
		return false, err
	}
	return n != "0", nil
}

// Lost reports whether removing a checkout would lose commits: HEAD's
// commits that no remote has, unless their changes are already in
// origin/<base> (MergedInto: merged, rebased or squash-merged, the branch
// since deleted on the server); and, for a shared clone (its branches live
// only in it), a stash or commits on its other branches. Uncommitted
// changes are Dirty's. An error means "can't tell".
func Lost(dir, base string) (bool, error) {
	if st, err := os.Stat(filepath.Join(dir, ".git")); err == nil && st.IsDir() {
		stash, err := Out(dir, "stash", "list")
		if err != nil || stash != "" {
			return true, err
		}
		other, err := Out(dir, "rev-list", "--count", "--branches", "--not", "--remotes", "HEAD")
		if err != nil || other != "0" {
			return true, err
		}
	}
	unpushed, err := UnpushedHead(dir)
	if err != nil || !unpushed {
		return unpushed, err
	}
	if base == "" || !HasRef(dir, "refs/remotes/origin/"+base) {
		return true, nil
	}
	merged, err := MergedInto(dir, "origin/"+base)
	return !merged, err
}

// MergedInto reports whether HEAD's changes since it left base are
// already in base, however they got there. Any of:
//   - the files HEAD changed since the merge base read the same in base
//     (merged or squash-merged, nothing in base touching them since);
//   - every commit's patch is in base (git cherry: rebased or
//     cherry-picked), on a branch without merge commits (cherry doesn't
//     see a merge's own changes);
//   - HEAD's whole change since the merge base is the patch of one commit
//     of base (git patch-id: squash-merged, base having moved on since).
//
// Each only says yes when nothing of HEAD's change is missing from base;
// a change that conflicted or was edited in the merge reads as not merged,
// which is the safe answer. It only reads.
func MergedInto(dir, base string) (bool, error) {
	mb, err := Out(dir, "merge-base", base, "HEAD")
	if err != nil {
		return false, err
	}
	names, err := Out(dir, "diff", "--name-only", "--no-renames", "-z", mb, "HEAD")
	if err != nil {
		return false, err
	}
	var files []string
	for _, f := range strings.Split(names, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return true, nil
	}
	if Ok(dir, append([]string{"diff", "--quiet", base, "HEAD", "--"}, files...)...) {
		return true, nil
	}
	// git cherry skips merge commits, whose own content (a conflict
	// resolution, an evil merge) would then go unseen: trust it only on a
	// branch without merges
	merges, err := Out(dir, "rev-list", "--count", "--merges", mb+"..HEAD")
	if err != nil {
		return false, err
	}
	if merges == "0" {
		if cherry, err := Out(dir, "cherry", base, "HEAD"); err == nil && !strings.Contains("\n"+cherry, "\n+") {
			return true, nil
		}
	}
	mine, err := patchIDs(dir, "diff", mb, "HEAD")
	if err != nil || len(mine) != 1 {
		return false, err
	}
	theirs, err := patchIDs(dir, "log", "--no-merges", "-p", "--format=commit %H", mb+".."+base)
	if err != nil {
		return false, err
	}
	for _, id := range theirs {
		if id == mine[0] {
			return true, nil
		}
	}
	return false, nil
}

// patchIDs is git patch-id --stable over the patches a git command prints.
func patchIDs(dir string, args ...string) ([]string, error) {
	patch, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("git", "-C", dir, "patch-id", "--stable")
	cmd.Stdin = strings.NewReader(string(patch))
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if id, _, ok := strings.Cut(l, " "); ok {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// SharedClone makes dest a clone of main that borrows its objects
// (git clone --shared: nothing is written to main) with main's refs and
// main's origin URL, so a push from it goes to the real remote.
func SharedClone(main, dest string) error {
	if err := exec.Command("git", "clone", "--quiet", "--shared", "--no-checkout", main, dest).Run(); err != nil {
		return err
	}
	if err := RefreshSharedClone(main, dest); err != nil {
		return err
	}
	// The configured URL, not get-url's (which applies url.insteadOf)
	url, err := Out(main, "config", "--get", "remote.origin.url")
	if err != nil {
		return err
	}
	return exec.Command("git", "-C", dest, "remote", "set-url", "origin", url).Run()
}

func RefreshSharedClone(main, dest string) error {
	return exec.Command("git", "-C", dest, "fetch", "--quiet", "--prune", main,
		"+refs/remotes/origin/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*").Run()
}

func InContainer() bool {
	for _, f := range []string{"/run/.containerenv", "/.dockerenv"} {
		if _, err := os.Stat(f); err == nil {
			return true
		}
	}
	return false
}

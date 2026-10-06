// Package gitx runs git: captured output for queries, passed-through
// output for the commands the user watches, and the shared clones that
// stand in for worktrees where a main clone's .git isn't writable.
package gitx

import (
	"context"
	"os"
	"os/exec"
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

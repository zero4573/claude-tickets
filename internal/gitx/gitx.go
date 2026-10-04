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

// Out runs git -C dir args and returns its stdout, trailing newline
// trimmed; stderr is discarded.
func Out(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimRight(string(out), "\n"), err
}

// Ok reports whether git -C dir args succeeds (output discarded).
func Ok(dir string, args ...string) bool {
	return exec.Command("git", append([]string{"-C", dir}, args...)...).Run() == nil
}

// Run runs git -C dir args with its output on ours.
func Run(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Timeout runs git -C dir args quietly, killed after d.
func Timeout(d time.Duration, dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Run()
}

// HasRef reports whether ref (e.g. refs/remotes/origin/main) exists.
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

// Dirty reports whether a checkout has uncommitted changes (extra: more
// status flags, e.g. --ignore-submodules).
func Dirty(dir string, extra ...string) bool {
	out, _ := Out(dir, append([]string{"status", "--porcelain"}, extra...)...)
	return out != ""
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

// RefreshSharedClone copies main's remote-tracking branches and tags into
// a shared clone.
func RefreshSharedClone(main, dest string) error {
	return exec.Command("git", "-C", dest, "fetch", "--quiet", "--prune", main,
		"+refs/remotes/origin/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*").Run()
}

// InContainer reports whether ct runs inside a container (podman or docker).
func InContainer() bool {
	for _, f := range []string{"/run/.containerenv", "/.dockerenv"} {
		if _, err := os.Stat(f); err == nil {
			return true
		}
	}
	return false
}

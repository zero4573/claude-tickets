// Package gitx runs git: captured output for queries, passed-through
// output for the commands the user watches, and the shared clones that
// stand in for worktrees where a main clone's .git isn't writable.
package gitx

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// safeFlags keep git from running code a repository controls, as a
// second layer under the config allowlist (allow.go, which refuses a repo
// whose own config holds any key off tix's safe list): a hook
// (core.hooksPath at the null device), a filesystem monitor command, a
// credential helper (the empty value resets the list), a proxy command, a
// signing program, a transport other than https and ssh, and recursion
// into submodules. A session can write a clone's .git/config and hooks,
// and tix's git runs on the host. core.sshCommand is overridden through
// GIT_SSH_COMMAND (see env), the external diff by --no-ext-diff on the
// diff and log commands that read patches.
func safeFlags() []string {
	null := "/dev/null"
	if runtime.GOOS == "windows" {
		null = "NUL"
	}
	return []string{"-c", "core.hooksPath=" + null, "-c", "core.fsmonitor=",
		"-c", "credential.helper=", "-c", "core.gitProxy=",
		"-c", "commit.gpgSign=false", "-c", "tag.gpgSign=false",
		"-c", "protocol.allow=never", "-c", "protocol.https.allow=always", "-c", "protocol.ssh.allow=always",
		"-c", "submodule.recurse=false", "-c", "fetch.recurseSubmodules=false", "-c", "diff.ignoreSubmodules=all"}
}

// localFlags let a git call read a local repository tix checked first (a
// shared clone's main clone) over the file transport.
func localFlags(args ...string) []string {
	return append([]string{"-c", "protocol.file.allow=always"}, args...)
}

// strippedEnv are variables that would point git at a repository or a
// config the allowlist never read; tix's own -c flags come after.
var strippedEnv = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_CONFIG",
	"GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES"}

// env is the environment of tix's git: GIT_SSH_COMMAND, which takes
// precedence over a repository's core.sshCommand, is the user's own value
// or plain ssh; the variables that relocate the repository or add config
// are dropped.
func env() []string {
	var e []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if stripped(k) {
			continue
		}
		e = append(e, kv)
	}
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		e = append(e, "GIT_SSH_COMMAND=ssh")
	}
	return e
}

func stripped(k string) bool {
	if runtime.GOOS == "windows" {
		k = strings.ToUpper(k)
	}
	for _, s := range strippedEnv {
		if k == s {
			return true
		}
	}
	return strings.HasPrefix(k, "GIT_CONFIG_KEY_") || strings.HasPrefix(k, "GIT_CONFIG_VALUE_")
}

// Command is exec.Command("git", args...) under the safe flags and env,
// refused (cmd.Err, which Start returns) unless the repository it runs in
// passes Check. Every git tix runs goes through it (or CommandContext,
// CommandIn). The repository is the leading -C dir; a call without one
// runs in a neutral folder (a clone's checked parent, else the file
// system root), so git never finds a repository from tix's own working
// directory. Callers never set cmd.Dir: CommandIn runs in a folder.
func Command(args ...string) *exec.Cmd {
	cmd := exec.Command("git")
	return guard(cmd, args)
}

// CommandContext is Command with a context.
func CommandContext(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git")
	return guard(cmd, args)
}

// CommandIn is Command run in dir: the one way to run git in a folder.
func CommandIn(dir string, args ...string) *exec.Cmd {
	return Command(append([]string{"-C", dir}, args...)...)
}

// Check refuses dir when tix wouldn't run git there: its git indirection
// files point somewhere they shouldn't (checkDir), or its repository's own
// config (and its submodules') holds a key off the safe list. It runs no
// git; a folder in no repository passes.
func Check(dir string) error {
	gitDir, common, err := locate(dir)
	if err != nil || gitDir == "" {
		return err
	}
	return checkConfig(dir, gitDir, common)
}

// guard fills cmd's arguments, env and folder for args and sets cmd.Err
// when they are refused.
func guard(cmd *exec.Cmd, args []string) *exec.Cmd {
	cmd.Env = env()
	args, dir, err := prepare(args)
	cmd.Args = append(append([]string{"git"}, safeFlags()...), args...)
	if err != nil {
		cmd.Err = err
		return cmd
	}
	cmd.Dir = dir
	return cmd
}

// prepare checks args and returns them (a fetch with its --refmap, a
// clone's paths absolute) with the folder git runs in.
func prepare(args []string) ([]string, string, error) {
	for _, a := range globalOpts(args) {
		if a == "--git-dir" || a == "--work-tree" || strings.HasPrefix(a, "--git-dir=") || strings.HasPrefix(a, "--work-tree=") {
			return args, "", fmt.Errorf("git: refusing %s: tix's git finds the repository through -C only", a)
		}
	}
	args, err := noSubmoduleChildren(args)
	if err != nil {
		return args, "", err
	}
	sub, _ := subcommand(args)
	if dir := dirOf(args); dir != "" {
		if err := Check(dir); err != nil {
			return args, "", err
		}
		if sub == "clone" {
			return args, "", fmt.Errorf("git: refusing clone with -C: tix clones through gitx.Clone")
		}
		return withRefmap(args), "", nil
	}
	if sub == "clone" {
		return prepareClone(args)
	}
	return args, neutralDir(), nil
}

// worktreeDiffs are the subcommands that compare the work tree and so
// would run git in each submodule (or in-tree gitlink repo, whose config
// Check never read) to see whether it is dirty.
var worktreeDiffs = map[string]bool{"status": true, "diff": true, "diff-files": true, "diff-index": true}

// noSubmoduleChildren keeps git from running itself in a submodule's work
// tree: worktreeDiffs get --ignore-submodules=all on the command line,
// last before any --, so it beats both submodule.<n>.ignore from
// .gitmodules (the -c diff.ignoreSubmodules=all of safeFlags doesn't) and
// an earlier option (parse-options takes abbreviations, the last value
// wins); any other --ignore-submodules value, abbreviated or not, refuses.
// add (any form: a pathspec diffs the work tree too), describe
// --dirty/--broken and commit -a, which take no such option and tix doesn't
// use, are refused, and so is worktree remove without --force (it checks
// the tree with status).
func noSubmoduleChildren(args []string) ([]string, error) {
	sub, rest := subcommand(args)
	switch {
	case worktreeDiffs[sub]:
		end := len(rest)
		for i, a := range rest {
			if a == "--" {
				end = i
				break
			}
			if name, val, eq := strings.Cut(a, "="); abbrevOf(name, "--ignore-submodules", len("--ignore-s")) && (eq && val != "all" || name != "--ignore-submodules") {
				return args, fmt.Errorf("git: refusing %s: tix's git ignores submodules", a)
			}
		}
		at := len(args) - len(rest) + end
		out := append([]string(nil), args[:at]...)
		out = append(out, "--ignore-submodules=all")
		return append(out, args[at:]...), nil
	case sub == "worktree" && len(rest) > 0 && rest[0] == "remove" && !hasOpt(rest[1:], "", "f", "--force"):
		return args, fmt.Errorf("git: refusing worktree remove without --force: it runs status in each submodule")
	case sub == "add",
		sub == "describe" && hasAbbrev(rest, "--dirty", "--broken"),
		sub == "commit" && hasOpt(rest, "mFcCtS", "a", "--all"):
		return args, fmt.Errorf("git: refusing %s %s: it would run git in each submodule; tix doesn't use it", sub, strings.Join(rest, " "))
	}
	return args, nil
}

// abbrevOf reports whether name is long or an abbreviation of it at least
// min bytes long, as parse-options would take it.
func abbrevOf(name, long string, min int) bool {
	return len(name) >= min && strings.HasPrefix(long, name)
}

// hasAbbrev reports whether rest (up to --) holds one of the long options
// or an abbreviation of it (git refuses an ambiguous one anyway).
func hasAbbrev(rest []string, long ...string) bool {
	for _, a := range rest {
		if a == "--" {
			return false
		}
		name, _, _ := strings.Cut(a, "=")
		for _, l := range long {
			if abbrevOf(name, l, len("--")+1) {
				return true
			}
		}
	}
	return false
}

// hasOpt reports whether rest (up to --) holds one of the long options
// (exact or with =) or one of the short letters, also inside a cluster of
// short options (-qA); a letter of valued names the rest of its cluster.
func hasOpt(rest []string, valued, short string, long ...string) bool {
	for _, a := range rest {
		if a == "--" {
			return false
		}
		if strings.HasPrefix(a, "--") {
			for _, l := range long {
				if a == l || strings.HasPrefix(a, l+"=") {
					return true
				}
			}
			continue
		}
		if !strings.HasPrefix(a, "-") {
			continue
		}
		for _, c := range a[1:] {
			if strings.ContainsRune(short, c) {
				return true
			}
			if strings.ContainsRune(valued, c) {
				break
			}
		}
	}
	return false
}

// neutralDir is the file system root, a folder no repository is in.
func neutralDir() string {
	if runtime.GOOS == "windows" {
		if v := filepath.VolumeName(os.TempDir()); v != "" {
			return v + `\`
		}
		return `C:\`
	}
	return "/"
}

// globalOpts are the arguments before the subcommand.
func globalOpts(args []string) []string {
	for i := 0; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "-") {
			return args[:i]
		}
		switch args[i] {
		case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path":
			i++
		}
	}
	return args
}

// subcommand is the git subcommand of args (past the global options) and
// the arguments after it.
func subcommand(args []string) (string, []string) {
	g := globalOpts(args)
	if len(g) == len(args) {
		return "", nil
	}
	return args[len(g)], args[len(g)+1:]
}

// withRefmap adds --refmap=+refs/heads/*:refs/remotes/<remote>/* to a
// fetch from a named remote, so the branches it writes are that remote's
// tracking branches whatever the config says. git takes --refmap only
// with refspecs on the command line: a fetch without any gets that same
// refspec, so the configured ones are never used.
func withRefmap(args []string) []string {
	sub, rest := subcommand(args)
	if sub != "fetch" {
		return args
	}
	remote, refspecs := "", 0
	for _, a := range rest {
		if strings.HasPrefix(a, "--refmap") || a == "--" {
			return args
		}
		if strings.HasPrefix(a, "-") {
			if !fetchFlags[a] {
				return args // an option that may take a value: leave it as is
			}
			continue
		}
		if remote == "" {
			remote = a
		} else {
			refspecs++
		}
	}
	if !remoteName(remote) {
		return args
	}
	spec := "+refs/heads/*:refs/remotes/" + remote + "/*"
	at := len(args) - len(rest)
	out := append([]string(nil), args[:at]...)
	out = append(out, "--refmap="+spec)
	out = append(out, args[at:]...)
	if refspecs == 0 {
		out = append(out, spec)
	}
	return out
}

// fetchFlags are fetch options that take no value.
var fetchFlags = map[string]bool{"-q": true, "--quiet": true, "-p": true, "--prune": true, "--prune-tags": true,
	"--no-tags": true, "-t": true, "--tags": true, "-f": true, "--force": true, "-v": true, "--verbose": true,
	"--dry-run": true, "--atomic": true, "--progress": true}

// cloneFlags are the clone options tix uses; the guard refuses any other.
var cloneFlags = map[string]bool{"-q": true, "--quiet": true, "-s": true, "--shared": true,
	"-n": true, "--no-checkout": true, "--progress": true, "--no-tags": true}

// prepareClone checks a clone: --no-checkout (the checkout is a second,
// checked call), a source that passes the URL rule or is a local
// repository that passes Check, and a destination whose parent passes
// Check. It runs in that parent, with both paths absolute.
func prepareClone(args []string) ([]string, string, error) {
	g := globalOpts(args)
	var pos []string
	noCheckout, dashdash := false, false
	for _, a := range args[len(g)+1:] {
		switch {
		case dashdash || !strings.HasPrefix(a, "-"):
			pos = append(pos, a)
		case a == "--":
			dashdash = true
		case cloneFlags[a]:
			noCheckout = noCheckout || a == "-n" || a == "--no-checkout"
		default:
			return args, "", fmt.Errorf("git: refusing clone option %s", strconv.Quote(a))
		}
	}
	if !noCheckout {
		return args, "", fmt.Errorf("git: refusing clone without --no-checkout")
	}
	if len(pos) != 2 {
		return args, "", fmt.Errorf("git: refusing clone: it needs a source and a destination")
	}
	src, dest := pos[0], pos[1]
	if !urlOK(src) {
		abs, err := filepath.Abs(src)
		if err != nil || !isDir(abs) {
			return args, "", fmt.Errorf("git: refusing clone of %s: not an https or ssh URL, nor a local repository", strconv.Quote(src))
		}
		if gd, _, err := locate(abs); err != nil {
			return args, "", err
		} else if gd == "" {
			return args, "", fmt.Errorf("git: refusing clone of %s: not a repository", abs)
		}
		if err := Check(abs); err != nil {
			return args, "", err
		}
		src = abs
	}
	dest, err := filepath.Abs(dest)
	if err != nil {
		return args, "", err
	}
	parent := filepath.Dir(dest)
	dir := neutralDir()
	if isDir(parent) {
		if err := Check(parent); err != nil {
			return args, "", err
		}
		dir = parent
	}
	out := append([]string(nil), args[:len(g)+1]...)
	for _, a := range args[len(g)+1:] {
		if strings.HasPrefix(a, "-") && a != "--" {
			out = append(out, a)
		}
	}
	return append(out, "--", src, dest), dir, nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// Clone clones url into dest (absent) and checks it out: git clone
// --no-checkout, then a checkout in the new clone, which tix checks like
// any other. A clone of an empty repository has nothing to check out.
func Clone(url, dest string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := Command("clone", "--no-checkout", "--", url, dest)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	if !HasRef(dest, "HEAD") {
		return nil
	}
	co := CommandIn(dest, "checkout", "--quiet")
	co.Stdout, co.Stderr = stdout, stderr
	return co.Run()
}

// Out trims stdout's trailing newline and discards stderr.
func Out(dir string, args ...string) (string, error) {
	out, err := Command(append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimRight(string(out), "\n"), err
}

// OutIn is Out with stdin (nil for none), and git's stderr in the error
// (in place of "exit status N") so a caller can show why git failed.
func OutIn(dir string, stdin io.Reader, args ...string) (string, error) {
	cmd := Command(append([]string{"-C", dir}, args...)...)
	cmd.Stdin = stdin
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
		return "", err
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// Lines runs git and calls fn with each line of its stdout (without the
// newline) as it arrives, so a large output is never held whole. Like
// OutIn, git's stderr is in the error.
func Lines(dir string, fn func(line string), args ...string) error {
	cmd := Command(append([]string{"-C", dir}, args...)...)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	r := bufio.NewReaderSize(out, 64<<10)
	for {
		l, rerr := r.ReadString('\n')
		if l != "" {
			fn(strings.TrimSuffix(l, "\n"))
		}
		if rerr != nil {
			break
		}
	}
	_, _ = io.Copy(io.Discard, out)
	if err := cmd.Wait(); err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}

func Ok(dir string, args ...string) bool {
	return Command(append([]string{"-C", dir}, args...)...).Run() == nil
}

func Run(dir string, args ...string) error {
	cmd := Command(append([]string{"-C", dir}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func Timeout(d time.Duration, dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return CommandContext(ctx, append([]string{"-C", dir}, args...)...).Run()
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

// Dirty reports whether dir's work tree has changes, untracked files
// included whatever status.showUntrackedFiles says (ignored files and
// submodule work trees aren't looked at).
func Dirty(dir string, extra ...string) bool {
	// A git that fails counts as dirty: callers use this to decide whether
	// work would be lost. No optional locks: a status shouldn't write the
	// index (tix clean --dry-run changes nothing)
	out, err := Out(dir, append([]string{"--no-optional-locks", "status", "--porcelain", "--untracked-files=normal"}, extra...)...)
	return err != nil || out != ""
}

// Unpushed reports whether a clone holds work no remote has: commits on HEAD
// (a detached one included) or on any local branch, or a stash. An error
// means "can't tell".
func Unpushed(dir string) (bool, error) {
	n, err := Out(dir, "rev-list", "--count", "HEAD", "--branches", "--not", "--remotes")
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
	names, err := Out(dir, "diff", "--no-ext-diff", "--no-textconv", "--name-only", "--no-renames", "-z", mb, "HEAD")
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
	if Ok(dir, append([]string{"diff", "--no-ext-diff", "--no-textconv", "--quiet", base, "HEAD", "--"}, files...)...) {
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
	mine, err := patchIDs(dir, "diff", "--no-ext-diff", "--no-textconv", mb, "HEAD")
	if err != nil || len(mine) != 1 {
		return false, err
	}
	theirs, err := patchIDs(dir, "log", "--no-ext-diff", "--no-textconv", "--no-merges", "-p", "--format=commit %H", mb+".."+base)
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

// patchIDs is git patch-id --stable over the patches a git command prints
// (callers pass --no-ext-diff --no-textconv: a repo's diff.external or
// textconv driver is a program its config names).
func patchIDs(dir string, args ...string) ([]string, error) {
	patch, err := Command(append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return nil, err
	}
	cmd := Command("-C", dir, "patch-id", "--stable")
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
//
// The clone's origin is main until its URL is set: that is written with
// git config -f on the new clone's config file, since a checked call in
// the clone would refuse a local-path origin.
func SharedClone(main, dest string) error {
	// The configured URL, not get-url's (which applies url.insteadOf)
	url, err := Out(main, "config", "--get", "remote.origin.url")
	if err != nil {
		return err
	}
	if err := Command(localFlags("clone", "--quiet", "--shared", "--no-checkout", main, dest)...).Run(); err != nil {
		return err
	}
	conf, err := filepath.Abs(filepath.Join(dest, ".git", "config"))
	if err != nil {
		return err
	}
	if err := Command("config", "-f", conf, "remote.origin.url", url).Run(); err != nil {
		return err
	}
	return RefreshSharedClone(main, dest)
}

// RefreshSharedClone fetches main's remote branches and tags into dest
// (main is checked first: git reads its config over the file transport).
func RefreshSharedClone(main, dest string) error {
	if err := Check(main); err != nil {
		return err
	}
	return Command(localFlags("-C", dest, "fetch", "--quiet", "--prune", main,
		"+refs/remotes/origin/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*")...).Run()
}

// ContainerOverride, when set, replaces the container check. Only test
// code sets it (the CLI testscripts, which can't tell a sandbox from the
// host); the shipped binary never does.
var ContainerOverride func() bool

func InContainer() bool {
	if ContainerOverride != nil {
		return ContainerOverride()
	}
	for _, f := range []string{"/run/.containerenv", "/.dockerenv"} {
		if _, err := os.Stat(f); err == nil {
			return true
		}
	}
	return false
}

// PopulatedSubmodule is the first submodule (or nested repository) of the
// work tree at dir that is checked out, "" when there is none: a gitlink
// whose path holds a .git, or a git dir with a non-empty modules/ (where a
// worktree's submodules keep their git dirs). It runs git only in dir, never
// in a submodule; a git that fails counts as one ("?").
func PopulatedSubmodule(dir string) string {
	gitDir, err := Out(dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "?"
	}
	out, err := Out(dir, "ls-files", "--stage", "-z")
	if err != nil {
		return "?"
	}
	for _, e := range strings.Split(out, "\x00") {
		meta, path, ok := strings.Cut(e, "\t")
		if !ok || !strings.HasPrefix(meta, "160000 ") {
			continue
		}
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(path), ".git")); err == nil {
			return path
		}
	}
	// a submodule whose gitlink is gone from the index still has its work here
	if ents, err := os.ReadDir(filepath.Join(gitDir, "modules")); err == nil && len(ents) > 0 {
		return filepath.Join(gitDir, "modules", ents[0].Name())
	}
	return ""
}

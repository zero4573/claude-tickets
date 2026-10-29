package gitx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/zero4573/claude-tickets/internal/fsx"
)

// maxConfigSize caps a config file ct reads: a real one is a few KiB.
const maxConfigSize = 256 << 10

// allowed reports whether git may read e from a repository's own config
// when ct runs it on the host. Every key is matched on its (section,
// subsection, name) triple; anything not named here is refused, and no
// allowed key names a program or a place git would run one from.
func allowed(e confEntry) bool {
	v := strings.TrimSpace(e.value)
	switch e.section {
	case "core":
		if e.hasSub {
			return false
		}
		switch e.name {
		case "repositoryformatversion":
			return e.hasValue && (v == "0" || v == "1")
		case "bare":
			return isFalse(e)
		case "filemode", "logallrefupdates", "ignorecase", "precomposeunicode", "symlinks",
			"autocrlf", "eol", "sparsecheckout", "sparsecheckoutcone", "untrackedcache", "commitgraph":
			return true
		}
	case "extensions":
		if e.hasSub {
			return false
		}
		switch e.name {
		case "worktreeconfig":
			return true
		case "objectformat":
			return v == "sha1" || v == "sha256"
		case "refstorage":
			return v == "files" || v == "reftable"
		case "partialclone":
			return e.hasValue && remoteName(v)
		}
	case "remote":
		if !e.hasSub {
			return e.name == "pushdefault" && e.hasValue && remoteName(v)
		}
		switch e.name {
		case "url", "pushurl":
			return e.hasValue && urlOK(e.value)
		case "fetch":
			return e.hasValue && fetchRefspecOK(v, e.sub)
		case "tagopt":
			return v == "--no-tags" || v == "--tags"
		case "prune", "promisor", "partialclonefilter", "gh-resolved":
			return true
		}
	case "branch":
		if !e.hasSub {
			return false
		}
		switch e.name {
		case "remote", "pushremote":
			return e.hasValue && (v == "." || remoteName(v))
		case "merge":
			return strings.HasPrefix(v, "refs/") && !hasControl(v)
		case "rebase":
			return rebaseOK(e)
		case "mergeoptions":
			return false
		}
		// git reads no other branch.<n> key: tool metadata (description,
		// vscode-merge-base, gk-last-accessed, ...)
		return true
	case "user":
		return !e.hasSub && (e.name == "name" || e.name == "email" || e.name == "signingkey")
	case "commit", "tag":
		return !e.hasSub && e.name == "gpgsign"
	case "gpg":
		return !e.hasSub && e.name == "format" && (v == "openpgp" || v == "ssh" || v == "x509")
	case "pull":
		return !e.hasSub && (e.name == "ff" || (e.name == "rebase" && rebaseOK(e)))
	case "push":
		return !e.hasSub && (e.name == "default" || e.name == "autosetupremote")
	case "fetch":
		return !e.hasSub && (e.name == "prune" || e.name == "writecommitgraph")
	case "init":
		return !e.hasSub && e.name == "defaultbranch"
	case "rerere":
		return !e.hasSub && e.name == "enabled"
	case "merge":
		return !e.hasSub && e.name == "conflictstyle"
	case "diff":
		return !e.hasSub && (e.name == "algorithm" || e.name == "renames")
	case "status":
		return !e.hasSub && e.name == "showuntrackedfiles"
	case "gc":
		return !e.hasSub && e.name == "auto"
	case "maintenance":
		return !e.hasSub && (e.name == "auto" || e.name == "strategy")
	case "index":
		return !e.hasSub && e.name == "version"
	case "feature":
		return !e.hasSub && e.name == "manyfiles"
	case "advice":
		return !e.hasSub
	case "color":
		return e.name != "pager"
	case "filter":
		if !e.hasSub || e.sub != "lfs" || !e.hasValue {
			return false
		}
		switch e.name {
		case "smudge":
			return v == "git-lfs smudge -- %f"
		case "clean":
			return v == "git-lfs clean -- %f"
		case "process":
			return v == "git-lfs filter-process"
		case "required":
			return v == "true"
		}
	case "lfs":
		if !e.hasSub {
			return e.name == "repositoryformatversion"
		}
		if !urlOK(e.sub) {
			return false
		}
		switch e.name {
		case "access":
			return true
		case "locksverify":
			return isBool(e)
		}
	case "submodule":
		if !e.hasSub {
			return e.name == "active"
		}
		switch e.name {
		case "url":
			return e.hasValue && urlOK(e.value)
		case "active", "branch":
			return true
		}
	}
	return false
}

func isFalse(e confEntry) bool {
	if !e.hasValue {
		return false // a bare key is true
	}
	switch strings.ToLower(strings.TrimSpace(e.value)) {
	case "false", "no", "off", "0", "":
		return true
	}
	return false
}

func isBool(e confEntry) bool {
	if !e.hasValue {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(e.value)) {
	case "true", "yes", "on", "1", "false", "no", "off", "0", "":
		return true
	}
	return false
}

// rebaseOK: pull.rebase and branch.<n>.rebase may be true, false or
// merges, never interactive (which opens an editor).
func rebaseOK(e confEntry) bool {
	return isBool(e) || strings.ToLower(strings.TrimSpace(e.value)) == "merges"
}

var remoteNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// remoteName: a remote's name, never a URL or a path.
func remoteName(s string) bool {
	return remoteNameRe.MatchString(s) && !strings.HasPrefix(s, "-")
}

func hasControl(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// fetchRefspecOK: a fetch refspec of remote n may only write under
// refs/remotes/<n>/, or fetch tags into refs/tags/; a negative one
// (^refs/...) only excludes. Anything else could overwrite (and with
// --prune delete) local branches.
func fetchRefspecOK(spec, n string) bool {
	if hasControl(spec) || strings.Contains(spec, "..") || strings.ContainsAny(spec, " \\") {
		return false
	}
	if strings.HasPrefix(spec, "^") {
		return strings.HasPrefix(spec, "^refs/") && !strings.Contains(spec, ":")
	}
	if spec == "+refs/tags/*:refs/tags/*" || spec == "refs/tags/*:refs/tags/*" {
		return true
	}
	src, dst, ok := strings.Cut(strings.TrimPrefix(spec, "+"), ":")
	if !ok || strings.Contains(dst, ":") || src == "" {
		return false
	}
	return strings.HasPrefix(dst, "refs/remotes/"+n+"/") && len(dst) > len("refs/remotes/"+n+"/")
}

var (
	schemeRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*::`)
	scpRe    = regexp.MustCompile(`^[A-Za-z0-9._~+-]+@(\[[0-9A-Za-z:.%]+\]|[A-Za-z0-9][A-Za-z0-9.-]*):`)
	hostRe   = regexp.MustCompile(`^([A-Za-z0-9._~%+][A-Za-z0-9._~%+-]*@)?(\[[0-9A-Za-z:.%]+\]|[A-Za-z0-9][A-Za-z0-9.-]*)(:[0-9]*)?(/|$)`)
)

// urlOK is the URL rule: https://host/..., ssh://[user@]host[:port]/...,
// or scp-like user@host:path (git reads a '/' before the first ':' as a
// local path). Never a <transport>:: helper, file:, a local path, a host
// alias without user@, anything starting with '-' or holding a control
// character.
func urlOK(u string) bool {
	if u == "" || hasControl(u) || strings.HasPrefix(u, "-") || strings.ContainsAny(u, " \\") ||
		schemeRe.MatchString(u) {
		return false
	}
	lu := strings.ToLower(u)
	for _, scheme := range []string{"https://", "ssh://"} {
		if strings.HasPrefix(lu, scheme) {
			return hostRe.MatchString(u[len(scheme):])
		}
	}
	if strings.Contains(u, "://") {
		return false
	}
	colon := strings.IndexByte(u, ':')
	slash := strings.IndexByte(u, '/')
	if colon < 0 || (slash >= 0 && slash < colon && !strings.HasPrefix(u, "[")) {
		return false
	}
	return scpRe.MatchString(u)
}

// configFiles are the config files git reads for a repository whose git
// dir is gitDir and common dir common: config and config.worktree of the
// common dir, the worktree's own config.worktree, and every submodule's
// under modules/ (recursively).
func configFiles(gitDir, common string) ([]string, error) {
	files := []string{filepath.Join(common, "config"), filepath.Join(common, "config.worktree")}
	if gitDir != common {
		files = append(files, filepath.Join(gitDir, "config.worktree"))
	}
	n := 0
	more, err := moduleConfigs(filepath.Join(common, "modules"), 0, &n)
	return append(files, more...), err
}

// Bounds of the modules/ walk: deeper or wider refuses.
const (
	maxModuleDepth = 32
	maxModuleDirs  = 10000
)

// moduleConfigs lists the config and config.worktree files of every git
// dir under dir (a submodule's name may hold '/', so its git dir can be
// nested). A symlink there refuses: git would follow it. A folder of a git
// dir is walked, whatever its name (hooks, info, ...), as a nested
// submodule's git dir may sit anywhere under one; objects/ is skipped
// (unless it is itself a git dir), as it holds no config and many folders,
// and so are refs/ and logs/ unless a HEAD sits anywhere below (loose refs
// and reflogs can be many folders; a symlink there, or a walk over its
// bounds, refuses). The walk is
// bounded by depth and by the number of folders visited (*n).
func moduleConfigs(dir string, depth int, n *int) ([]string, error) {
	if depth > maxModuleDepth {
		return nil, fmt.Errorf("%s: submodule folders nested over %d deep", dir, maxModuleDepth)
	}
	if *n++; *n > maxModuleDirs {
		return nil, fmt.Errorf("%s: over %d folders under modules/", dir, maxModuleDirs)
	}
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	gitDir := isGitDir(dir)
	var files []string
	if gitDir {
		files = append(files, filepath.Join(dir, "config"), filepath.Join(dir, "config.worktree"))
	}
	for _, e := range ents {
		p := filepath.Join(dir, e.Name())
		if e.Type()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s is a symlink", p)
		}
		if !e.IsDir() || (gitDir && e.Name() == "objects" && !isGitDirectory(p) && !isGitDir(p)) {
			continue
		}
		if gitDir && (e.Name() == "refs" || e.Name() == "logs") && !isGitDir(p) {
			found, err := headBelow(p)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
		}
		more, err := moduleConfigs(p, depth+1, n)
		if err != nil {
			return nil, err
		}
		files = append(files, more...)
	}
	return files, nil
}

// maxHeadSearch caps the folders headBelow lists under one refs/ or logs/.
const maxHeadSearch = 100000

// headBelow reports whether a git dir sits in dir or in any folder below
// it, up to maxModuleDepth levels. (A bare HEAD file doesn't count: every
// submodule's logs/HEAD and refs/remotes/origin/HEAD are one.) Loose refs
// and reflogs are many folders and hold no git dir, so a folder without one
// is skipped. It fails closed: a
// symlink, a folder nested too deep or more than maxHeadSearch folders is an
// error, never a skip.
func headBelow(dir string) (bool, error) {
	budget := maxHeadSearch
	var walk func(d string, depth int) (bool, error)
	walk = func(d string, depth int) (bool, error) {
		if depth > maxModuleDepth {
			return false, fmt.Errorf("%s: folders nested over %d deep", d, maxModuleDepth)
		}
		if budget--; budget < 0 {
			return false, fmt.Errorf("%s: over %d folders", dir, maxHeadSearch)
		}
		if isGitDir(d) {
			return true, nil
		}
		ents, err := os.ReadDir(d)
		if err != nil {
			return false, err
		}
		found := false
		for _, e := range ents {
			p := filepath.Join(d, e.Name())
			if e.Type()&fs.ModeSymlink != 0 {
				return false, fmt.Errorf("%s is a symlink", p)
			}
			if !e.IsDir() {
				continue
			}
			ok, err := walk(p, depth+1)
			if err != nil {
				return false, err
			}
			found = found || ok
		}
		return found, nil
	}
	return walk(dir, 0)
}

// isGitDir is git's is_git_directory, roughly: a HEAD file and an objects
// folder (or a commondir file, for a worktree's git dir).
func isGitDir(dir string) bool {
	if st, err := os.Lstat(filepath.Join(dir, "HEAD")); err != nil || st.IsDir() {
		return false
	}
	if st, err := os.Lstat(filepath.Join(dir, "objects")); err == nil && st.IsDir() {
		return true
	}
	_, err := os.Lstat(filepath.Join(dir, "commondir"))
	return err == nil
}

// readConfig reads one config file: missing means no keys; a symlink, a
// non-regular file or one over maxConfigSize refuses.
func readConfig(path string) ([]confEntry, error) {
	st, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if st.Size() > maxConfigSize {
		return nil, fmt.Errorf("%s is over %d KiB", path, maxConfigSize>>10)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > maxConfigSize {
		return nil, fmt.Errorf("%s is over %d KiB", path, maxConfigSize>>10)
	}
	return parseConfig(data, path)
}

// submoduleWorktree allows the core.worktree git writes into a
// submodule's config: a relative path from that config's folder to a
// folder inside the superproject's work tree (top, the folder whose
// .git/modules holds the config).
func submoduleWorktree(e confEntry, top string) bool {
	if e.section != "core" || e.hasSub || e.name != "worktree" || !e.hasValue ||
		hasControl(e.value) || filepath.IsAbs(e.value) || top == "" {
		return false
	}
	wt := filepath.Clean(filepath.Join(filepath.Dir(e.file), filepath.FromSlash(e.value)))
	return fsx.Inside(wt, top)
}

// superproject is the work tree whose .git/modules holds the config file
// f (the nearest such folder above f), or "" when f isn't under one: a
// submodule's own config under its superproject's modules/, whether ct
// runs in the superproject or in the submodule's work tree.
func superproject(f string) string {
	for d := filepath.Dir(f); filepath.Dir(d) != d; d = filepath.Dir(d) {
		if filepath.Base(d) == "modules" && filepath.Base(filepath.Dir(d)) == ".git" {
			return filepath.Dir(filepath.Dir(d))
		}
	}
	return ""
}

// maxListed caps the keys a refusal names.
const maxListed = 10

// checkConfig reads every config file of the repository (git dir gitDir,
// common dir common) and refuses, naming repo, when one can't be read or
// parsed or holds a key allowed doesn't pass.
func checkConfig(repo, gitDir, common string) error {
	files, err := configFiles(gitDir, common)
	if err != nil {
		return fmt.Errorf("ct: %s: can't check its git config: %w", repo, err)
	}
	var bad []confEntry
	for _, f := range files {
		ents, err := readConfig(f)
		if err != nil {
			return fmt.Errorf("ct: %s: can't check its git config: %w", repo, err)
		}
		for _, e := range ents {
			if !allowed(e) && !submoduleWorktree(e, superproject(f)) {
				bad = append(bad, e)
			}
		}
	}
	if len(bad) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ct: %s: git config not on ct's safe list:\n", repo)
	for i, e := range bad {
		if i == maxListed {
			fmt.Fprintf(&b, "  ... and %d more\n", len(bad)-maxListed)
			break
		}
		fmt.Fprintf(&b, "  %s at %s:%d\n", strconv.Quote(e.key()), e.file, e.line)
	}
	fmt.Fprintf(&b, "inspect with: git -C %s config --show-origin --list", repo)
	return errors.New(b.String())
}

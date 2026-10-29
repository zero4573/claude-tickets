package gitx

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// entries parses a config for the allowlist tables.
func entries(t *testing.T, conf string) []confEntry {
	t.Helper()
	ents, err := parseConfig([]byte(conf), "config")
	if err != nil {
		t.Fatalf("parse %q: %v", conf, err)
	}
	return ents
}

func TestAllowed(t *testing.T) {
	for _, c := range []string{
		"[core]\nrepositoryformatversion = 0\nrepositoryformatversion = 1\nfilemode = true\nbare = false\nbare = no\nlogallrefupdates = true\nignorecase = true\nprecomposeunicode = true\nsymlinks = false\nautocrlf = input\neol = lf\nsparseCheckout = true\nsparseCheckoutCone = true\nuntrackedCache = true\ncommitGraph = true\n",
		"[extensions]\nworktreeConfig = true\nobjectFormat = sha256\nobjectformat = sha1\nrefStorage = reftable\nrefstorage = files\npartialClone = origin\n",
		"[remote \"origin\"]\nurl = git@github.com:acme/x.git\npushurl = ssh://git@example.com:2222/acme/x.git\nfetch = +refs/heads/*:refs/remotes/origin/*\nfetch = refs/heads/main:refs/remotes/origin/main\nfetch = ^refs/heads/tmp/*\nfetch = +refs/tags/*:refs/tags/*\nfetch = refs/tags/*:refs/tags/*\ntagOpt = --no-tags\nprune = true\npromisor = true\npartialclonefilter = blob:none\ngh-resolved = base\n",
		"[remote \"up\"]\nurl = https://example.com/acme/x.git\nurl = https://user@example.com/x\nurl = git@[::1]:x.git\n[remote]\npushDefault = origin\n",
		"[branch \"main\"]\nremote = origin\npushRemote = up\nmerge = refs/heads/main\nrebase = merges\nrebase = true\ndescription = the main line\nvscode-merge-base = origin/main\ngk-last-accessed = 2026-01-01\n[branch \"x\"]\nremote = .\n",
		"[user]\nname = Jane Doe\nemail = jane@example.com\nsigningkey = ABC\n[commit]\ngpgsign = true\n[tag]\ngpgSign = true\n[gpg]\nformat = ssh\n",
		"[pull]\nrebase = false\nff = only\n[push]\ndefault = current\nautoSetupRemote = true\n[fetch]\nprune = true\nwriteCommitGraph = true\n[init]\ndefaultBranch = main\n[rerere]\nenabled = true\n[merge]\nconflictstyle = zdiff3\n",
		"[diff]\nalgorithm = histogram\nrenames = true\n[status]\nshowUntrackedFiles = all\n[gc]\nauto = 0\n[maintenance]\nauto = false\nstrategy = incremental\n[index]\nversion = 4\n[feature]\nmanyFiles = true\n",
		"[advice]\ndetachedHead = false\n[color]\nui = auto\n[color \"diff\"]\nmeta = blue\n",
		"[filter \"lfs\"]\nsmudge = git-lfs smudge -- %f\nclean = git-lfs clean -- %f\nprocess = git-lfs filter-process\nrequired = true\n[filter.LFS]\nrequired = true\n",
		"[lfs]\nrepositoryformatversion = 0\n[lfs \"https://example.com/acme/x.git/info/lfs\"]\naccess = basic\nlocksverify = false\n[lfs \"git@github.com:acme/x.git\"]\nlocksverify = true\n",
		"[submodule]\nactive = .\n[submodule \"lib\"]\nurl = https://example.com/lib.git\nactive = true\nbranch = main\n",
	} {
		for _, e := range entries(t, c) {
			if !allowed(e) {
				t.Errorf("%s = %q refused", e.key(), e.value)
			}
		}
	}
}

func TestRefused(t *testing.T) {
	for _, c := range []string{
		// programs and where git finds them
		"[core]\nsshCommand = evil", "[core]\nfsmonitor = evil", "[core]\nhooksPath = /tmp/h",
		"[core]\naskPass = evil", "[core]\ngitProxy = evil", "[core]\npager = evil", "[core]\neditor = evil",
		"[core]\nworktree = /tmp/x", "[core]\nbare = true", "[core]\nbare", "[core]\nrepositoryformatversion = 2",
		"[core \"x\"]\nbare = false", "[credential]\nhelper = evil", "[pager]\nls-remote = evil",
		"[diff]\nexternal = evil", "[diff \"conv\"]\ntextconv = evil", "[diff \"algorithm\"]\ntextconv = evil",
		"[merge \"x\"]\ndriver = evil", "[sequence]\neditor = evil", "[gpg]\nprogram = evil", "[gpg]\nformat = evil",
		"[gpg \"ssh\"]\nprogram = evil", "[uploadpack]\npackObjectsHook = evil", "[protocol \"ext\"]\nallow = always",
		"[url \"ext::sh -c evil\"]\ninsteadOf = https://", "[http]\nproxy = http://evil", "[include]\npath = /tmp/c",
		"[includeIf \"gitdir:/\"]\npath = /tmp/c", "[alias]\nst = !evil", "[extensions]\nobjectformat = md5",
		"[extensions]\npartialclone = ext::x", "[extensions]\nfoo = bar",
		// remotes
		"[remote \"origin\"]\nuploadpack = evil", "[remote \"origin\"]\nreceivepack = evil",
		"[remote \"origin\"]\nurl = ext::sh -c evil", "[remote \"origin\"]\nurl = fd::3",
		"[remote \"origin\"]\nurl = file:///tmp/r", "[remote \"origin\"]\nurl = /tmp/r", "[remote \"origin\"]\nurl = ./r",
		"[remote \"origin\"]\nurl = ../r", "[remote \"origin\"]\nurl = C:\\\\r", "[remote \"origin\"]\nurl = -oProxyCommand=evil",
		"[remote \"origin\"]\nurl = alias:acme/x", "[remote \"origin\"]\nurl = http://example.com/x",
		"[remote \"origin\"]\nurl = git://example.com/x", "[remote \"origin\"]\nurl = ssh://-oProxyCommand=evil/x",
		"[remote \"origin\"]\nurl = ssh://-oProxy@host/x", "[remote \"origin\"]\nurl = \"https://example.com/x\\n\"",
		"[remote \"origin\"]\nurl = git@host/x:y", "[remote \"origin\"]\nurl", "[remote \"origin\"]\nvcs = evil",
		"[remote \"origin\"]\nfetch = +refs/heads/*:refs/heads/*", "[remote \"origin\"]\nfetch = refs/heads/main",
		"[remote \"origin\"]\nfetch = +refs/heads/*:refs/remotes/up/*", "[remote \"origin\"]\nfetch = +refs/heads/*:refs/remotes/origin/../../heads/*",
		"[remote \"origin\"]\nfetch = +refs/*:refs/*", "[remote \"origin\"]\npush = refs/heads/*:refs/heads/*",
		"[remote \"origin\"]\ntagopt = --evil", "[remote]\npushdefault = ext::x",
		// branches
		"[branch \"main\"]\nremote = ext::sh -c evil", "[branch \"main\"]\nremote = /tmp/r", "[branch \"main\"]\nremote = -x",
		"[branch \"main\"]\npushremote = a/b", "[branch \"main\"]\nmerge = main", "[branch \"main\"]\nrebase = interactive",
		"[branch \"main\"]\nmergeoptions = -s evil", "[branch]\nautosetupmerge = always", "[pull]\nrebase = interactive",
		"[pull \"x\"]\nff = only",
		// lfs
		"[filter \"lfs\"]\nsmudge = /evil", "[filter \"lfs\"]\nclean = \"git-lfs clean -- %f; evil\"", "[filter \"lfs\"]\nprocess = evil",
		"[filter \"lfs\"]\nrequired = false", "[filter \"LFS\"]\nsmudge = git-lfs smudge -- %f", "[filter \"x\"]\nclean = evil",
		"[filter \"lfs\"]\nsmudge", "[lfs]\nurl = https://evil", "[lfs]\nstandalonetransferagent = x",
		"[lfs \"customtransfer.x.path\"]\naccess = basic", "[lfs \"https://example.com\"]\nstandalonetransferagent = x",
		"[lfs \"customtransfer\"]\npath = evil", "[lfs \"extension\"]\nclean = evil", "[lfs \"https://example.com\"]\nlocksverify = maybe",
		// submodules
		"[submodule \"lib\"]\nupdate = !evil", "[submodule \"lib\"]\nurl = ext::sh -c evil", "[submodule \"lib\"]\nurl = /tmp/r",
		"[submodule]\nrecurse = true", "[submodule \"lib\"]\nfetchrecursesubmodules = true",
		"[color]\npager = true", "[color \"x\"]\npager = true", "[advice \"x\"]\ny = z",
	} {
		ents := entries(t, c)
		if len(ents) == 0 {
			t.Fatalf("%q: no keys", c)
		}
		for _, e := range ents {
			if allowed(e) {
				t.Errorf("%s = %q allowed", e.key(), e.value)
			}
		}
	}
}

func TestURLRule(t *testing.T) {
	for u, want := range map[string]bool{
		"https://github.com/acme/x.git":   true,
		"HTTPS://github.com/acme/x":       true,
		"ssh://git@github.com/acme/x.git": true,
		"ssh://github.com:22/acme/x":      true,
		"ssh://[::1]:22/x":                true,
		"git@github.com:acme/x.git":       true,
		"git@bitbucket.org:acme/x.git":    true,
		"git@[::1]:x":                     true,
		"https://":                        false,
		"https:///x":                      false,
		"ssh://":                          false,
		"github.com:acme/x":               false,
		"[::1]:x":                         false,
		"ext::ssh -c x":                   false,
		"fd::7":                           false,
		"transport::https://x":            false,
		"file:///tmp/x":                   false,
		"/tmp/x":                          false,
		"x":                               false,
		"./x:y":                           false,
		"-u@host:x":                       false,
		"git@-host:x":                     false,
		"git@host:x\x7f":                  false,
		"git@host:x y":                    false,
		"":                                false,
	} {
		if got := urlOK(u); got != want {
			t.Errorf("urlOK(%q) = %t, want %t", u, got, want)
		}
	}
}

// The keys VS Code, GitKraken, git-lfs and submodules write pass.
func TestAllowedRealClones(t *testing.T) {
	conf := `[core]
	repositoryformatversion = 0
	filemode = true
	bare = false
	logallrefupdates = true
[remote "origin"]
	url = git@bitbucket.org:acme/billing.git
	fetch = +refs/heads/*:refs/remotes/origin/*
[branch "main"]
	remote = origin
	merge = refs/heads/main
	vscode-merge-base = origin/main
	gk-last-accessed = 2026-01-01T00:00:00.000Z
[lfs "https://bitbucket.org/acme/billing.git/info/lfs"]
	locksverify = false
[lfs]
	repositoryformatversion = 0
[submodule]
	active = .
[submodule "lib"]
	url = git@bitbucket.org:acme/lib.git
	active = true
[filter "lfs"]
	smudge = git-lfs smudge -- %f
	process = git-lfs filter-process
	required = true
	clean = git-lfs clean -- %f
`
	for _, e := range entries(t, conf) {
		if !allowed(e) {
			t.Errorf("%s = %q refused", e.key(), e.value)
		}
	}
}

// fakeRepo is a main clone layout (no git run) with config as its config.
func fakeRepo(t *testing.T, config string) (repo, dotgit string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "r")
	dotgit = filepath.Join(repo, ".git")
	if err := os.MkdirAll(filepath.Join(dotgit, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dotgit, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dotgit, "HEAD"), "ref: refs/heads/main\n")
	write(t, filepath.Join(dotgit, "config"), config)
	return repo, dotgit
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckFiles(t *testing.T) {
	ok := "[core]\n\tbare = false\n"
	repo, dotgit := fakeRepo(t, ok)
	if err := Check(repo); err != nil {
		t.Fatalf("a clean repo: %v", err)
	}
	if err := Check(filepath.Join(repo, "sub", "dir")); err != nil {
		t.Fatalf("a folder in a clean repo: %v", err)
	}
	if err := Check(t.TempDir()); err != nil {
		t.Fatalf("a folder in no repo: %v", err)
	}
	refused := func(what, want string) {
		t.Helper()
		err := Check(repo)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want an error with %q", what, err, want)
		}
	}
	// config.worktree is read without extensions.worktreeConfig
	write(t, filepath.Join(dotgit, "config.worktree"), "[core]\nfsmonitor = evil\n")
	refused("config.worktree", `"core.fsmonitor" at `+filepath.Join(dotgit, "config.worktree")+":2")
	os.Remove(filepath.Join(dotgit, "config.worktree"))
	// a submodule's config, nested names too
	write(t, filepath.Join(dotgit, "modules", "a", "b", "HEAD"), "x\n")
	os.MkdirAll(filepath.Join(dotgit, "modules", "a", "b", "objects"), 0o755)
	write(t, filepath.Join(dotgit, "modules", "a", "b", "config"), "[filter \"x\"]\nclean = evil\n")
	refused("a submodule's config", `"filter.x.clean"`)
	// its core.worktree must lead into the superproject's work tree
	write(t, filepath.Join(dotgit, "modules", "a", "b", "config"), "[core]\nworktree = ../../../../a/b\n")
	if err := Check(repo); err != nil {
		t.Errorf("a submodule's own core.worktree: %v", err)
	}
	write(t, filepath.Join(dotgit, "modules", "a", "b", "config"), "[core]\nworktree = ../../../../../elsewhere\n")
	refused("a submodule's core.worktree out of the work tree", `"core.worktree"`)
	write(t, filepath.Join(dotgit, "config"), "[core]\nworktree = ../x\n")
	write(t, filepath.Join(dotgit, "modules", "a", "b", "config"), ok)
	refused("core.worktree in the superproject", `"core.worktree"`)
	// a branch named config is not a config file
	write(t, filepath.Join(dotgit, "config"), ok)
	write(t, filepath.Join(dotgit, "modules", "a", "b", "refs", "heads", "config"), "0123\n")
	if err := Check(repo); err != nil {
		t.Errorf("a submodule branch named config: %v", err)
	}
	// symlinks, non-regular files, size
	os.Remove(filepath.Join(dotgit, "config"))
	if err := os.Symlink(filepath.Join(t.TempDir(), "c"), filepath.Join(dotgit, "config")); err == nil {
		refused("a symlinked config", "not a regular file")
		os.Remove(filepath.Join(dotgit, "config"))
	}
	os.Mkdir(filepath.Join(dotgit, "config"), 0o755)
	refused("a config folder", "not a regular file")
	os.Remove(filepath.Join(dotgit, "config"))
	write(t, filepath.Join(dotgit, "config"), ok+strings.Repeat("# pad\n", 50000))
	refused("a big config", "KiB")
	write(t, filepath.Join(dotgit, "config"), "[core\n")
	refused("a parse error", "can't check its git config")
}

func TestRefusalMessage(t *testing.T) {
	var conf strings.Builder
	conf.WriteString("[remote \"x';id;'\"]\nfoo = 1\n[remote \"y\\\"z\"]\nbar = 2\n")
	for i := 0; i < 12; i++ {
		conf.WriteString("[core]\npager = less\n")
	}
	repo, dotgit := fakeRepo(t, conf.String())
	err := Check(repo)
	if err == nil {
		t.Fatal("not refused")
	}
	msg := err.Error()
	lines := strings.Split(msg, "\n")
	if lines[0] != "ct: "+repo+": git config not on ct's safe list:" {
		t.Errorf("first line %q", lines[0])
	}
	if lines[1] != `  "remote.x';id;'.foo" at `+filepath.Join(dotgit, "config")+":2" {
		t.Errorf("key line %q", lines[1])
	}
	if lines[2] != `  "remote.y\"z.bar" at `+filepath.Join(dotgit, "config")+":4" {
		t.Errorf("escaped key line %q", lines[2])
	}
	if len(lines) != 1+10+1+1 || !strings.Contains(lines[11], "and 4 more") {
		t.Errorf("%d lines, want 10 keys and a count: %q", len(lines), msg)
	}
	if last := lines[len(lines)-1]; last != "inspect with: git -C "+repo+" config --show-origin --list" {
		t.Errorf("last line %q", last)
	}
}

// A submodule git dir nested under another's is checked even when its
// name is one of a git dir's internal folders.
func TestNestedSubmoduleInternalName(t *testing.T) {
	_, dotgit := fakeRepo(t, "[core]\n\tbare = false\n")
	outer := filepath.Join(dotgit, "modules", "a")
	write(t, filepath.Join(outer, "HEAD"), "ref: refs/heads/main\n")
	os.MkdirAll(filepath.Join(outer, "objects"), 0o755)
	os.MkdirAll(filepath.Join(outer, "refs", "heads"), 0o755)
	for _, name := range []string{"refs", "objects", "hooks", "logs", "info", "refs/x"} {
		inner := filepath.Join(outer, name)
		write(t, filepath.Join(inner, "HEAD"), "ref: refs/heads/main\n")
		os.MkdirAll(filepath.Join(inner, "objects"), 0o755)
		os.MkdirAll(filepath.Join(inner, "refs"), 0o755)
		write(t, filepath.Join(inner, "config"), "[filter \"x\"]\n\tclean = evil\n")
		files, err := moduleConfigs(filepath.Join(dotgit, "modules"), 0, new(int))
		if err != nil || !slices.Contains(files, filepath.Join(inner, "config")) {
			t.Errorf("%s: config not walked: %v %v", name, files, err)
		}
		if name == "refs" {
			if err := Check(filepath.Dir(dotgit)); err == nil || !strings.Contains(err.Error(), "filter.x.clean") {
				t.Errorf("planted key under refs: %v", err)
			}
		}
		os.RemoveAll(inner)
	}
}

func TestModuleWalkBounds(t *testing.T) {
	dir := t.TempDir()
	deep := dir
	for i := 0; i <= maxModuleDepth+1; i++ {
		deep = filepath.Join(deep, "d")
	}
	os.MkdirAll(deep, 0o755)
	if _, err := moduleConfigs(dir, 0, new(int)); err == nil || !strings.Contains(err.Error(), "deep") {
		t.Errorf("depth bound: %v", err)
	}
	n := maxModuleDirs
	if _, err := moduleConfigs(t.TempDir(), 0, &n); err == nil || !strings.Contains(err.Error(), "folders") {
		t.Errorf("count bound: %v", err)
	}
}

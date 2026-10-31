package gitx

import (
	"cmp"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// refusedRun runs gitx's git in repo and fails the test unless it was
// refused naming want, and the planted program never ran.
func refusedRun(t *testing.T, repo, marker, want string, args ...string) {
	t.Helper()
	_, err := OutIn(repo, nil, args...)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("git %v: %v, want a refusal naming %s", args, err, want)
	}
	if ran(marker) {
		t.Errorf("git %v ran the planted program", args)
	}
}

func TestRefusesPlantedPrograms(t *testing.T) {
	repo, marker, script := plantedRepo(t)
	for _, c := range []struct{ key, value, want string }{
		{"remote.origin.uploadpack", script, `"remote.origin.uploadpack"`},
		{"core.askPass", script, `"core.askpass"`},
		{"core.gitProxy", script, `"core.gitproxy"`},
		{"protocol.ext.allow", "always", `"protocol.ext.allow"`},
		{"pager.status", script, `"pager.status"`},
		{"credential.helper", "!" + script, `"credential.helper"`},
	} {
		git(t, repo, "config", c.key, c.value)
		refusedRun(t, repo, marker, c.want, "fetch", "origin")
		refusedRun(t, repo, marker, c.want, "status")
		git(t, repo, "config", "--unset", c.key)
	}
	for _, u := range []string{filepath.Dir(repo), "ext::sh -c " + script, "file://" + filepath.Dir(repo)} {
		git(t, repo, "config", "remote.origin.url", u)
		refusedRun(t, repo, marker, `"remote.origin.url"`, "fetch", "origin")
	}
	git(t, repo, "config", "remote.origin.url", "git@example.invalid:x.git")
	git(t, repo, "config", "branch.main.remote", "ext::sh -c "+script)
	refusedRun(t, repo, marker, `"branch.main.remote"`, "pull")
	git(t, repo, "config", "--unset", "branch.main.remote")
	if _, err := Out(repo, "rev-parse", "--git-dir"); err != nil {
		t.Fatalf("the cleaned repo: %v", err)
	}
	// A key hidden after a comment ending in a backslash (git reads it)
	conf := filepath.Join(repo, ".git", "config")
	data, _ := os.ReadFile(conf)
	write(t, conf, string(data)+"[core]\n# note \\\nsshCommand = "+script+"\n")
	refusedRun(t, repo, marker, `"core.sshcommand"`, "status")
}

// A repo's own filter.lfs.smudge that isn't git-lfs's runs on checkout.
func TestLfsNamedFilterRefused(t *testing.T) {
	repo, marker, _ := plantedRepo(t)
	write(t, filepath.Join(repo, "a.txt"), "a\n")
	write(t, filepath.Join(repo, ".gitattributes"), "* filter=lfs\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "x")
	filt := filepath.Join(filepath.Dir(repo), "f.sh")
	if err := os.WriteFile(filt, []byte("#!/bin/sh\ntouch "+marker+"\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "config", "filter.lfs.smudge", filt)
	os.Remove(filepath.Join(repo, "a.txt"))
	refusedRun(t, repo, marker, `"filter.lfs.smudge"`, "checkout", "--", "a.txt")
}

func TestDotGitSymlinkRefused(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	d := t.TempDir()
	root, outside := filepath.Join(d, "root"), filepath.Join(d, "outside")
	os.MkdirAll(filepath.Join(root, "wt"), 0o755)
	git(t, d, "init", "-q", outside)
	if err := os.Symlink(filepath.Join(outside, ".git"), filepath.Join(root, "wt", ".git")); err != nil {
		t.Skip("no symlinks here")
	}
	if err := checkDir(filepath.Join(root, "wt")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf(".git symlink, no roots: %v", err)
	}
	TrustRoots(root)
	defer TrustRoots()
	if err := checkDir(filepath.Join(root, "wt")); err == nil {
		t.Errorf(".git symlink to a repo outside the trusted roots passed checkDir")
	}
}

// A fetch refspec writing refs/heads/* would delete local branches on
// tix's fetch --prune.
func TestBranchDeletingRefspecRefused(t *testing.T) {
	clone, wt := repo(t)
	git(t, clone, "branch", "keep")
	git(t, clone, "config", "--add", "remote.origin.fetch", "+refs/heads/*:refs/heads/*")
	if err := Timeout(30e9, clone, "fetch", "--quiet", "--prune", "origin"); err == nil {
		t.Error("fetch ran with a refspec into refs/heads/")
	}
	git(t, clone, "config", "--unset", "remote.origin.fetch", `refs/heads/\*:refs/heads/\*`)
	if !HasRef(clone, "refs/heads/keep") || !HasRef(wt, "refs/heads/feature/PROJ-12") {
		t.Error("a local branch went")
	}
	// The second layer: tix's fetch names its own refspec and refmap
	if err := Timeout(30e9, clone, "fetch", "--quiet", "--prune", "origin"); err != nil {
		t.Fatalf("a clean fetch: %v", err)
	}
}

func TestWithRefmap(t *testing.T) {
	m := "--refmap=+refs/heads/*:refs/remotes/origin/*"
	s := "+refs/heads/*:refs/remotes/origin/*"
	for _, c := range []struct{ in, want []string }{
		{[]string{"-C", "d", "fetch", "--quiet", "--prune", "origin"}, []string{"-C", "d", "fetch", m, "--quiet", "--prune", "origin", s}},
		{[]string{"-C", "d", "fetch", "--quiet", "origin", "main"}, []string{"-C", "d", "fetch", m, "--quiet", "origin", "main"}},
		{[]string{"-C", "d", "fetch", "/main/clone", "+refs/x:refs/x"}, []string{"-C", "d", "fetch", "/main/clone", "+refs/x:refs/x"}},
		{[]string{"-C", "d", "fetch", "--depth", "1", "origin"}, []string{"-C", "d", "fetch", "--depth", "1", "origin"}},
		{[]string{"-C", "d", "status"}, []string{"-C", "d", "status"}},
	} {
		if got := withRefmap(c.in); !slices.Equal(got, c.want) {
			t.Errorf("withRefmap(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A filter planted in a submodule's own config ran on the superproject's
// status.
func TestSubmoduleConfigChecked(t *testing.T) {
	sup, marker, _ := plantedRepo(t)
	lib := filepath.Join(filepath.Dir(sup), "lib")
	git(t, filepath.Dir(sup), "init", "-q", lib)
	write(t, filepath.Join(lib, "f"), "a\n")
	write(t, filepath.Join(lib, ".gitattributes"), "f filter=x\n")
	git(t, lib, "add", ".")
	git(t, lib, "commit", "-qm", "x")
	git(t, sup, "submodule", "add", "-q", remoteURL(t, filepath.Dir(sup), "lib"), "lib")
	git(t, sup, "commit", "-qm", "sub")
	if err := Check(sup); err != nil {
		t.Fatalf("a plain submodule: %v", err)
	}
	filt := filepath.Join(filepath.Dir(sup), "filt")
	os.WriteFile(filt, []byte("#!/bin/sh\ntouch "+marker+"\ncat\n"), 0o755)
	git(t, filepath.Join(sup, "lib"), "config", "filter.x.clean", filt)
	os.WriteFile(filepath.Join(sup, "lib", "f"), []byte("b\n"), 0o644)
	_ = exec.Command("git", "-C", sup, "status", "--porcelain").Run()
	if !ran(marker) {
		t.Skip("plain git doesn't run the submodule's filter here; nothing to prove")
	}
	os.Remove(marker)
	refusedRun(t, sup, marker, `"filter.x.clean"`, "status", "--porcelain")
}

// A call without -C runs in a neutral folder: git doesn't find the repo
// of tix's working directory (where a pager or a filter could be planted).
func TestNoDirRunsNeutral(t *testing.T) {
	repo, _, script := plantedRepo(t)
	git(t, repo, "config", "pager.ls-remote", script)
	wd, _ := os.Getwd()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	cmd := Command("ls-remote", "git@example.invalid:x.git")
	if cmd.Dir != neutralDir() || cmd.Err != nil {
		t.Fatalf("Dir %q, Err %v; want %q", cmd.Dir, cmd.Err, neutralDir())
	}
	if out, err := Command("rev-parse", "--show-toplevel").Output(); err == nil {
		t.Errorf("git found a repository from tix's cwd: %s", out)
	}
	if c := CommandIn(repo, "status"); !slices.Contains(c.Args, "-C") || c.Err == nil {
		t.Errorf("CommandIn: %q, %v", c.Args, c.Err)
	}
}

func TestEnvAndArgsCantRelocate(t *testing.T) {
	clone, _ := repo(t)
	evil, _, _ := plantedRepo(t)
	git(t, evil, "config", "core.fsmonitor", "evil")
	for _, k := range []string{"GIT_DIR", "GIT_COMMON_DIR"} {
		t.Setenv(k, filepath.Join(evil, ".git"))
	}
	t.Setenv("GIT_WORK_TREE", evil)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.fsmonitor")
	t.Setenv("GIT_CONFIG_VALUE_0", "evil")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.fsmonitor'='evil'")
	t.Setenv("GIT_CONFIG", filepath.Join(evil, ".git", "config"))
	t.Setenv("GIT_OBJECT_DIRECTORY", filepath.Join(evil, ".git", "objects"))
	t.Setenv("GIT_ALTERNATE_OBJECT_DIRECTORIES", filepath.Join(evil, ".git", "objects"))
	for _, kv := range env() {
		k, _, _ := strings.Cut(kv, "=")
		if stripped(k) {
			t.Errorf("env keeps %s", kv)
		}
	}
	got, err := Out(clone, "rev-parse", "--absolute-git-dir")
	if err != nil || got != resolve(filepath.Join(clone, ".git")) {
		t.Errorf("git dir %q, %v; want the clone's", got, err)
	}
	for _, a := range [][]string{{"--git-dir", evil}, {"--git-dir=" + evil}, {"--work-tree", evil}, {"--work-tree=" + evil}} {
		if err := Command(append(append([]string{"-C", clone}, a...), "status")...).Run(); err == nil || !strings.Contains(err.Error(), "-C only") {
			t.Errorf("%v: %v", a, err)
		}
	}
}

func TestClone(t *testing.T) {
	clone, _ := repo(t)
	root := filepath.Dir(clone)
	dest := filepath.Join(root, "new")
	if err := Clone("git@example.invalid:remote.git", dest, nil, nil, nil); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if out, err := Out(dest, "log", "--format=%s"); err != nil || out != "first" {
		t.Errorf("the new clone: %q, %v", out, err)
	}
	if Dirty(dest) {
		t.Error("the new clone isn't checked out")
	}
	empty := filepath.Join(root, "empty.git")
	git(t, root, "init", "-q", "--bare", empty)
	if err := Clone("git@example.invalid:empty.git", filepath.Join(root, "e"), nil, nil, nil); err != nil {
		t.Errorf("an empty repository: %v", err)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"clone", "git@example.invalid:remote.git", filepath.Join(root, "a")}, "--no-checkout"},
		{[]string{"clone", "--no-checkout", "ext::sh -c x", filepath.Join(root, "a")}, "not an https or ssh URL"},
		{[]string{"clone", "--no-checkout", "--upload-pack=x", "git@example.invalid:remote.git", filepath.Join(root, "a")}, "option"},
		{[]string{"clone", "--no-checkout", "--template", "x", "git@example.invalid:remote.git", filepath.Join(root, "a")}, "option"},
		{[]string{"-C", root, "clone", "--no-checkout", "git@example.invalid:remote.git", "a"}, "gitx.Clone"},
		{[]string{"clone", "--no-checkout", t.TempDir(), filepath.Join(root, "a")}, "not a repository"},
	} {
		if err := Command(c.args...).Run(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want %q", c.args, err, c.want)
		}
	}
	// a local source is a repository that passes the check
	git(t, clone, "config", "uploadpack.packObjectsHook", "evil")
	if err := Command("clone", "--no-checkout", clone, filepath.Join(root, "b")).Run(); err == nil || !strings.Contains(err.Error(), "uploadpack") {
		t.Errorf("a local source with a planted key: %v", err)
	}
	git(t, clone, "config", "--unset", "uploadpack.packObjectsHook")
	// a shared clone takes main's origin URL
	if err := SharedClone(clone, filepath.Join(root, "shared")); err != nil {
		t.Fatalf("SharedClone: %v", err)
	}
	if u, err := Out(filepath.Join(root, "shared"), "config", "--get", "remote.origin.url"); err != nil || u != "git@example.invalid:remote.git" {
		t.Errorf("shared clone origin %q, %v", u, err)
	}
}

// Nothing sets Dir on a gitx command: CommandIn runs in a folder. In a
// package where any file imports gitx, every file is checked: each x.Dir =
// assignment must be on an x the same function last took from exec.Command
// or exec.CommandContext, and no exec.Cmd literal sets Dir, so a gitx
// command kept in a struct field, returned by a helper (in another file)
// or reassigned can't be given a Dir either.
func TestNoDirOnGitxCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("one OS is enough")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	pkgs := map[string][]goFile{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") && p != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if filepath.Dir(p) == filepath.Join(root, "internal", "gitx") {
			if !strings.HasSuffix(p, "_test.go") && strings.Contains(string(data), ".Dir =") && filepath.Base(p) != "gitx.go" {
				t.Errorf("%s sets Dir", p)
			}
			return nil
		}
		pkgs[filepath.Dir(p)] = append(pkgs[filepath.Dir(p)], goFile{p, data})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, files := range pkgs {
		calls, bad, err := dirOnGitx(files...)
		if err != nil {
			t.Fatal(err)
		}
		n += calls
		for _, b := range bad {
			t.Errorf("%s sets Dir on a command not from exec.Command; use gitx.CommandIn", b)
		}
	}
	if n == 0 {
		t.Error("found no gitx commands: the walk is broken")
	}
}

type goFile struct {
	path string
	src  []byte
}

// dirOnGitx parses the Go files of one package and, when any of them
// imports gitx, returns how many gitx.Command* calls they make and where
// they set Dir on a command: an x.Dir = on an x its function (or
// package-level func literal) didn't last take from exec.Command or
// exec.CommandContext, or an exec.Cmd literal with a Dir field.
func dirOnGitx(files ...goFile) (calls int, bad []string, err error) {
	fset := token.NewFileSet()
	type parsed struct {
		f             *ast.File
		gitx, execPkg string
	}
	var all []parsed
	imports := false
	for _, gf := range files {
		f, err := parser.ParseFile(fset, gf.path, gf.src, 0)
		if err != nil {
			return 0, nil, err
		}
		pf := parsed{f: f, execPkg: "exec"}
		for _, im := range f.Imports {
			ip, _ := strconv.Unquote(im.Path.Value)
			name := ""
			if im.Name != nil {
				name = im.Name.Name
			}
			switch {
			case strings.HasSuffix(ip, "/internal/gitx"):
				pf.gitx = cmp.Or(name, "gitx")
				imports = true
			case ip == "os/exec":
				pf.execPkg = cmp.Or(name, "exec")
			}
		}
		all = append(all, pf)
	}
	if !imports {
		return 0, nil, nil
	}
	isSel := func(e ast.Expr, pkg string, names ...string) bool {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		return ok && id.Name == pkg && slices.Contains(names, sel.Sel.Name)
	}
	isCall := func(e ast.Expr, pkg string, names ...string) bool {
		c, ok := e.(*ast.CallExpr)
		return ok && isSel(c.Fun, pkg, names...)
	}
	for _, pf := range all {
		// body checks one function: fromExec holds the names whose last
		// assignment was an exec.Command
		body := func(b *ast.BlockStmt) {
			fromExec := map[string]bool{}
			set := func(l, r ast.Expr) {
				if r != nil && isCall(r, pf.execPkg, "Command", "CommandContext") {
					fromExec[types.ExprString(l)] = true
				} else {
					delete(fromExec, types.ExprString(l))
				}
			}
			ast.Inspect(b, func(nd ast.Node) bool {
				switch nd := nd.(type) {
				case *ast.ValueSpec:
					for i, l := range nd.Names {
						var r ast.Expr
						if len(nd.Values) == len(nd.Names) {
							r = nd.Values[i]
						}
						set(l, r)
					}
				case *ast.AssignStmt:
					for i, l := range nd.Lhs {
						if sel, ok := l.(*ast.SelectorExpr); ok && sel.Sel.Name == "Dir" && !fromExec[types.ExprString(sel.X)] {
							bad = append(bad, fset.Position(sel.Pos()).String())
						}
						var r ast.Expr
						if len(nd.Rhs) == len(nd.Lhs) {
							r = nd.Rhs[i]
						}
						set(l, r)
					}
				}
				return true
			})
		}
		ast.Inspect(pf.f, func(nd ast.Node) bool {
			if c, ok := nd.(ast.Expr); ok && pf.gitx != "" && isCall(c, pf.gitx, "Command", "CommandContext", "CommandIn") {
				calls++
			}
			if cl, ok := nd.(*ast.CompositeLit); ok && isSel(cl.Type, pf.execPkg, "Cmd") {
				for _, e := range cl.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok && types.ExprString(kv.Key) == "Dir" {
						bad = append(bad, fset.Position(kv.Pos()).String())
					}
				}
			}
			return true
		})
		for _, d := range pf.f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok {
				if fn.Body != nil {
					body(fn.Body)
				}
				continue
			}
			// package-level func literals (var f = func() {...})
			ast.Inspect(d, func(nd ast.Node) bool {
				if fl, ok := nd.(*ast.FuncLit); ok {
					body(fl.Body)
					return false
				}
				return true
			})
		}
	}
	return calls, bad, nil
}

// The guard's check itself: a gitx command in a struct field or from a
// helper is caught, an exec.Command's Dir isn't.
func TestDirOnGitxCatches(t *testing.T) {
	src := `package x
import (
	"os/exec"
	"example.com/m/internal/gitx"
)
type r struct{ cmd *exec.Cmd }
func helper() *exec.Cmd { return gitx.Command("status") }
func a() {
	v := r{cmd: gitx.Command("status")}
	v.cmd.Dir = "/x"
	h := helper()
	h.Dir = "/x"
	c := exec.Command("go", "build")
	c.Dir = "/x"
	g := gitx.CommandIn("/x", "log")
	g.Dir = "/y"
}
`
	calls, bad, err := dirOnGitx(goFile{"x.go", []byte(src)})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(bad) != 3 {
		t.Errorf("calls %d, flagged %v; want 3 and the lines of v.cmd, h and g", calls, bad)
	}
}

// A package's other files are checked too: a helper returning a gitx
// command in one file, its Dir set in a file that doesn't import gitx; an
// exec.Cmd literal with Dir; a package-level func literal; and a name
// reassigned from exec.Command to a gitx command.
func TestDirOnGitxTwoFiles(t *testing.T) {
	helper := `package x
import "example.com/m/internal/gitx"
import "os/exec"
func helper() *exec.Cmd { return gitx.Command("status") }
`
	other := `package x
import (
	"os"
	run "os/exec"
)
var f = func() {
	c := helper()
	c.Dir = "/a"
}
func b() {
	c := run.Command("go")
	c.Dir = "/ok"
	c = helper()
	c.Dir = "/b"
	var d = run.Command("go")
	d.Dir = "/ok"
	_ = &run.Cmd{Path: "git", Dir: "/c"}
	_ = os.Args
}
`
	calls, bad, err := dirOnGitx(goFile{"helper.go", []byte(helper)}, goFile{"other.go", []byte(other)})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(bad)
	want := []string{"other.go:14:", "other.go:17:", "other.go:8:"}
	if calls != 1 || len(bad) != len(want) {
		t.Fatalf("calls %d, flagged %v; want 1 and %v", calls, bad, want)
	}
	for i, w := range want {
		if !strings.HasPrefix(bad[i], w) {
			t.Errorf("flagged %v; want %v", bad, want)
			break
		}
	}
	if _, bad, _ := dirOnGitx(goFile{"other.go", []byte(other)}); bad != nil {
		t.Errorf("a package without gitx: flagged %v", bad)
	}
}

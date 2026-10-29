package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// gitDirOf is the git dir plain git discovers from dir ("" when none).
func gitDirOf(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--absolute-git-dir")
	cmd.Env = append(env(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return resolve(strings.TrimSpace(string(out)))
}

// locate takes the git dir git's own discovery takes: a .git folder or a
// bare-looking folder that fails is_git_directory is passed over, so the
// config Check reads is the config git uses.
func TestLocateMirrorsGitDiscovery(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	top := filepath.Join(t.TempDir(), "top")
	git(t, filepath.Dir(top), "init", "-q", top)
	git(t, top, "config", "filter.x.clean", "evil")
	hex40 := strings.Repeat("0123456789", 4)
	gitdir := func(d, head string, objects, refs bool) {
		write(t, filepath.Join(d, "HEAD"), head)
		if objects {
			os.MkdirAll(filepath.Join(d, "objects"), 0o755)
		}
		if refs {
			os.MkdirAll(filepath.Join(d, "refs"), 0o755)
		}
	}
	layouts := map[string]func(d string){
		"an empty .git folder":           func(d string) { os.MkdirAll(filepath.Join(d, ".git"), 0o755) },
		"a .git without refs":            func(d string) { gitdir(filepath.Join(d, ".git"), "ref: refs/heads/main\n", true, false) },
		"a .git without objects":         func(d string) { gitdir(filepath.Join(d, ".git"), "ref: refs/heads/main\n", false, true) },
		"a .git with a bad HEAD":         func(d string) { gitdir(filepath.Join(d, ".git"), "ref: heads/main\n", true, true) },
		"a .git with a short HEAD id":    func(d string) { gitdir(filepath.Join(d, ".git"), hex40[:39]+"\n", true, true) },
		"a full .git":                    func(d string) { gitdir(filepath.Join(d, ".git"), "ref: refs/heads/main\n", true, true) },
		"a .git, HEAD ref without space": func(d string) { gitdir(filepath.Join(d, ".git"), "ref:refs/heads/x", true, true) },
		"a .git, HEAD ref after tabs":    func(d string) { gitdir(filepath.Join(d, ".git"), "ref:\t\n refs/x\n", true, true) },
		"a .git, detached sha1 HEAD":     func(d string) { gitdir(filepath.Join(d, ".git"), strings.ToUpper(hex40)+"junk", true, true) },
		"a .git, detached sha256 HEAD":   func(d string) { gitdir(filepath.Join(d, ".git"), hex40+hex40[:24]+"\n", true, true) },
		"HEAD and objects only":          func(d string) { gitdir(d, "ref: refs/heads/main\n", true, false) },
		"a bare folder":                  func(d string) { gitdir(d, hex40+"\n", true, true) },
		"a bare folder, HEAD a folder":   func(d string) { os.MkdirAll(filepath.Join(d, "HEAD"), 0o755); gitdir(d+"/x", "", true, true) },
		"refs a plain file": func(d string) {
			gitdir(filepath.Join(d, ".git"), "ref: refs/heads/main\n", true, false)
			write(t, filepath.Join(d, ".git", "refs"), "")
		},
	}
	if runtime.GOOS != "windows" {
		layouts["a .git, HEAD a symlink into refs"] = func(d string) {
			gitdir(filepath.Join(d, ".git"), "", true, true)
			os.Remove(filepath.Join(d, ".git", "HEAD"))
			os.Symlink("refs/heads/main", filepath.Join(d, ".git", "HEAD"))
		}
		layouts["a .git, HEAD a symlink elsewhere"] = func(d string) {
			gitdir(filepath.Join(d, ".git"), "", true, true)
			os.Remove(filepath.Join(d, ".git", "HEAD"))
			os.Symlink("../HEAD", filepath.Join(d, ".git", "HEAD"))
		}
	}
	i := 0
	for name, mk := range layouts {
		i++
		d := filepath.Join(top, "l"+string(rune('a'+i)))
		mk(d)
		run := filepath.Join(d, "sub")
		os.MkdirAll(run, 0o755)
		want := gitDirOf(t, run)
		got, _, err := locate(run)
		if err != nil {
			t.Errorf("%s: locate: %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("%s: locate takes %q, git takes %q", name, got, want)
		}
		checkErr := Check(run)
		if usesTop := want == resolve(filepath.Join(top, ".git")); usesTop != (checkErr != nil) {
			t.Errorf("%s: git uses %s, Check: %v", name, want, checkErr)
		}
	}
}

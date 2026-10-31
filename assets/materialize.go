package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/zero4573/tix/internal/config"
	"github.com/zero4573/tix/internal/fsx"
)

// Materialize unpacks an embedded tree (the plugin) into the cache,
// at <cache>/<name>-<hash>, and returns that directory. The hash covers the
// files and, for the plugin, the path of this executable (its hooks run tix
// by that path), so a new version or a moved tix gets a fresh copy. Copies
// untouched for 30 days are removed.
func Materialize(name string) (string, error) {
	exe := ""
	if name == "plugin" {
		var err error
		if exe, err = executable(); err != nil {
			return "", err
		}
	}
	hash, err := treeHash(name, exe)
	if err != nil {
		return "", err
	}
	cache := config.CacheDir()
	dir := filepath.Join(cache, name+"-"+hash)
	now := time.Now()
	if fsx.IsDir(dir) {
		_ = os.Chtimes(dir, now, now)
		return dir, nil
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(cache, "."+name+"-")
	if err != nil {
		return "", err
	}
	if err := unpack(name, tmp, exe); err != nil {
		_ = fsx.RemoveAll(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil {
		_ = fsx.RemoveAll(tmp)
		if fsx.IsDir(dir) { // another tix got there first
			return dir, nil
		}
		return "", err
	}
	old, _ := filepath.Glob(filepath.Join(cache, name+"-*"))
	for _, d := range old {
		if d != dir && fsx.OlderThanDays(d, 30) {
			_ = fsx.RemoveAll(d)
		}
	}
	return dir, nil
}

// MaterializeTo copies the plugin tree at rel (a slash path inside the
// checkout, e.g. assets/plugin) into dir, replacing the copy there, with
// its hooks.json rewritten to run exe by path (HooksFor). Nothing in it
// runs. Every read goes through an os.Root at checkout, so nothing outside
// the checkout can be reached, even by a link swapped in during the copy.
// It refuses a tree without a plugin manifest, and an existing dir that
// isn't a plugin (no .claude-plugin/plugin.json): it never deletes
// anything else. It copies regular files and dirs only: the tree being a
// symlink, or a symlink or other special file in it, is an error naming it
// (a link could pull a file from outside the checkout into the copy).
func MaterializeTo(checkout, rel, dir, exe string) error {
	src := filepath.Join(checkout, filepath.FromSlash(rel))
	root, err := os.OpenRoot(checkout)
	if err != nil {
		return err
	}
	defer root.Close()
	st, err := root.Lstat(filepath.FromSlash(rel))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("no plugin at %s", src)
	case err != nil:
		return fmt.Errorf("%s: %w (it must stay inside %s)", src, err, checkout)
	case st.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%s is a symlink, not copying it", src)
	}
	if mf, err := root.Stat(filepath.FromSlash(path.Join(rel, ".claude-plugin", "plugin.json"))); err != nil || !mf.Mode().IsRegular() {
		return fmt.Errorf("no plugin at %s (no .claude-plugin/plugin.json)", src)
	}
	if _, err := os.Lstat(dir); err == nil && !isPlugin(dir) {
		return fmt.Errorf("%s isn't a plugin dir (no .claude-plugin/plugin.json), not replacing it", dir)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dir)+"-")
	if err != nil {
		return err
	}
	if err := unpackFS(root.FS(), rel, tmp, exe, readRooted(root)); err != nil {
		_ = fsx.RemoveAll(tmp)
		return fmt.Errorf("copying %s: %w", src, err)
	}
	if err := fsx.RemoveAll(dir); err != nil {
		_ = fsx.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, dir); err != nil {
		_ = fsx.RemoveAll(tmp)
		return err
	}
	return nil
}

// readRooted reads a file of root (p a slash path) after checking, on the
// open handle, that it is a regular file. On unix it opens without
// blocking, so a FIFO swapped in can't hang the copy.
func readRooted(root *os.Root) func(p string) ([]byte, fs.FileMode, error) {
	return func(p string) ([]byte, fs.FileMode, error) {
		flag := os.O_RDONLY
		if runtime.GOOS != "windows" {
			flag |= syscall.O_NONBLOCK
		}
		f, err := root.OpenFile(filepath.FromSlash(p), flag, 0)
		if err != nil {
			return nil, 0, err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return nil, 0, err
		}
		if !st.Mode().IsRegular() {
			return nil, 0, fmt.Errorf("%s isn't a regular file (%s), not copying it", p, st.Mode().Type())
		}
		data, err := io.ReadAll(f)
		return data, st.Mode(), err
	}
}

// isPlugin: dir is a directory with a plugin manifest.
func isPlugin(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	return err == nil && st.Mode().IsRegular()
}

// Executable is this tix's path, links resolved: what hooks run.
func Executable() (string, error) { return executable() }

func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe, nil
}

func treeHash(name, extra string) (string, error) {
	h := sha256.New()
	h.Write([]byte(extra + "\x00"))
	err := fs.WalkDir(Files, name, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := Files.ReadFile(p)
		if err != nil {
			return err
		}
		h.Write([]byte(p + "\x00"))
		h.Write(data)
		h.Write([]byte{0})
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:12], err
}

func unpack(name, dir, exe string) error {
	if name != "plugin" {
		exe = ""
	}
	return unpackFS(Files, name, dir, exe, func(p string) ([]byte, fs.FileMode, error) {
		data, err := Files.ReadFile(p)
		return data, 0o644, err
	})
}

// unpackFS writes the tree at root of fsys into dir; with exe set (a
// plugin), its hooks/hooks.json runs exe by path (HooksFor). It takes
// regular files and dirs only: any other entry (a symlink, a device) is
// an error naming it. read returns a file's data and mode.
func unpackFS(fsys fs.FS, root, dir, exe string, read func(p string) ([]byte, fs.FileMode, error)) error {
	return fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := p
		if root != "." {
			rel = strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
		} else if p == "." {
			rel = ""
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&fs.ModeType != 0 {
			return fmt.Errorf("%s isn't a regular file or a dir (%s), not copying it", p, d.Type())
		}
		data, got, err := read(p)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if path.Ext(p) == ".sh" || path.Base(path.Dir(p)) == "hooks" && path.Ext(p) == "" {
			mode = 0o755
		}
		if got.IsRegular() && got.Perm()&0o100 != 0 {
			mode = 0o755
		}
		if exe != "" && rel == "hooks/hooks.json" {
			data = []byte(HooksFor(string(data), exe))
		}
		return os.WriteFile(target, data, mode)
	})
}

// HooksFor rewrites the plugin's hooks.json so its hooks run tix by path
// ("tix hook ..." becomes "\"<exe>\" hook ..."): a session's PATH may not
// have tix.
func HooksFor(hooks, exe string) string {
	quoted, _ := json.Marshal(`"` + exe + `" hook `)
	return strings.ReplaceAll(hooks, `"tix hook `, string(quoted[:len(quoted)-1]))
}

package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/fsx"
)

// Materialize unpacks an embedded tree (plugin or graph) into the cache,
// at <cache>/<name>-<hash>, and returns that directory. The hash covers the
// files and, for the plugin, the path of this executable (its hooks run ct
// by that path), so a new version or a moved ct gets a fresh copy. Copies
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
		if fsx.IsDir(dir) { // another ct got there first
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
	return fs.WalkDir(Files, name, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, name), "/")
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := Files.ReadFile(p)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if path.Ext(p) == ".sh" || path.Base(path.Dir(p)) == "hooks" && path.Ext(p) == "" {
			mode = 0o755
		}
		if name == "plugin" && rel == "hooks/hooks.json" {
			data = []byte(HooksFor(string(data), exe))
		}
		return os.WriteFile(target, data, mode)
	})
}

// HooksFor rewrites the plugin's hooks.json so its hooks run ct by path
// ("ct hook ..." becomes "\"<exe>\" hook ..."): a session's PATH may not
// have ct.
func HooksFor(hooks, exe string) string {
	quoted, _ := json.Marshal(`"` + exe + `" hook `)
	return strings.ReplaceAll(hooks, `"ct hook `, string(quoted[:len(quoted)-1]))
}

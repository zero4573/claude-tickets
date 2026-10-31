package assets

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMaterialize(t *testing.T) {
	t.Setenv("TIX_CACHE", t.TempDir())
	dir, err := Materialize("plugin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatal("the plugin manifest isn't there:", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hooks map[string]any
	if err := json.Unmarshal(data, &hooks); err != nil {
		t.Fatalf("hooks.json isn't valid JSON after the rewrite: %v", err)
	}
	exe, _ := executable()
	if !strings.Contains(string(data), `"command": "\"`+strings.ReplaceAll(exe, `\`, `\\`)+`\" hook agent-state`) {
		t.Errorf("hooks don't run this tix:\n%s", data)
	}
	if again, _ := Materialize("plugin"); again != dir {
		t.Errorf("a second call unpacked again: %s", again)
	}

}

func TestHooksFor(t *testing.T) {
	got := HooksFor(`{"command": "tix hook agent-state idle"}`, `C:\Program Files\tix.exe`)
	var v map[string]string
	if err := json.Unmarshal([]byte(got), &v); err != nil {
		t.Fatal(err)
	}
	if v["command"] != `"C:\Program Files\tix.exe" hook agent-state idle` {
		t.Errorf("command = %q", v["command"])
	}
}

// pluginSrc is a checkout's plugin tree: the embedded one, on disk.
func pluginSrc(t *testing.T) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "assets", "plugin")
	if err := unpack("plugin", src, ""); err != nil {
		t.Fatal(err)
	}
	return src
}

// copyFrom copies the plugin tree at src, rooted at its parent.
func copyFrom(src, dir, exe string) error {
	return MaterializeTo(filepath.Dir(src), filepath.Base(src), dir, exe)
}

func TestMaterializeTo(t *testing.T) {
	src := pluginSrc(t)
	dir := filepath.Join(t.TempDir(), "dev", "plugin")
	if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "stale.md")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join("ws", ".cache", "dev", "bin", "tix")
	if err := copyFrom(src, dir, exe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatal("the plugin manifest isn't there:", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a file of the old copy is still there: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hooks map[string]any
	if err := json.Unmarshal(data, &hooks); err != nil {
		t.Fatalf("hooks.json isn't valid JSON after the rewrite: %v", err)
	}
	if !strings.Contains(string(data), strings.ReplaceAll(exe, `\`, `\\`)+`\" hook `) {
		t.Errorf("hooks don't run the given tix:\n%s", data)
	}
	if strings.Contains(string(data), `"tix hook `) {
		t.Errorf("a hook still runs tix from PATH:\n%s", data)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), ".plugin-*"))
	if len(left) > 0 {
		t.Errorf("temporary copies left: %v", left)
	}
}

func TestMaterializeToRefusesANonPluginDir(t *testing.T) {
	src := pluginSrc(t)
	dir := filepath.Join(t.TempDir(), "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(keep, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFrom(src, dir, "tix"); err == nil {
		t.Fatal("replaced a dir that isn't a plugin")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("a file of the refused dir is gone: %v", err)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), ".x-*"))
	if len(left) > 0 {
		t.Errorf("temporary copies left: %v", left)
	}
	// A file in its place is refused too
	file := filepath.Join(t.TempDir(), "plugin")
	if err := os.WriteFile(file, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFrom(src, file, "tix"); err == nil {
		t.Error("replaced a file")
	}
}

func TestMaterializeToRefusesASourceWithoutManifest(t *testing.T) {
	src := t.TempDir()
	dir := filepath.Join(t.TempDir(), "plugin")
	if err := copyFrom(src, dir, "tix"); err == nil {
		t.Fatal("copied a tree without a plugin manifest")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dir written: %v", err)
	}
}

func TestMaterializeToRefusesSymlinks(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A link in the tree to a file outside it
	src := pluginSrc(t)
	if err := os.Symlink(secret, filepath.Join(src, "leak")); err != nil {
		t.Skip("can't make symlinks here:", err)
	}
	dir := filepath.Join(t.TempDir(), "plugin")
	err := copyFrom(src, dir, "tix")
	if err == nil || !strings.Contains(err.Error(), "leak") {
		t.Errorf("copied a tree with a symlink in it, or the error doesn't name it: %v", err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Errorf("dir written: %v", err)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), ".plugin-*"))
	if len(left) > 0 {
		t.Errorf("temporary copies left: %v", left)
	}
	// src itself a link to a plugin tree
	link := filepath.Join(t.TempDir(), "plugin-link")
	if err := os.Symlink(pluginSrc(t), link); err != nil {
		t.Skip("can't make symlinks here:", err)
	}
	dir = filepath.Join(t.TempDir(), "plugin")
	err = copyFrom(link, dir, "tix")
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("copied through a symlinked src: %v", err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Errorf("dir written: %v", err)
	}
}

func TestMaterializeToRefusesALinkOutsideTheRoot(t *testing.T) {
	// assets is a link to a plugin tree outside the checkout: only the
	// root can tell, the tree itself is clean
	checkout := t.TempDir()
	outside := filepath.Dir(pluginSrc(t))
	if err := os.Symlink(outside, filepath.Join(checkout, "assets")); err != nil {
		t.Skip("can't make symlinks here:", err)
	}
	dir := filepath.Join(t.TempDir(), "plugin")
	err := MaterializeTo(checkout, "assets/plugin", dir, "tix")
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Errorf("copied through a link outside the checkout: %v", err)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("dir written: %v", err)
	}
	// a link inside the checkout is followed
	in := t.TempDir()
	if err := os.Rename(outside, filepath.Join(in, "real")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(in, "assets")); err != nil {
		t.Fatal(err)
	}
	if err := MaterializeTo(in, "assets/plugin", dir, "tix"); err != nil {
		t.Errorf("a link inside the checkout refused: %v", err)
	}
	if err := MaterializeTo(in, "nothing/plugin", filepath.Join(t.TempDir(), "p"), "tix"); err == nil || !strings.Contains(err.Error(), "no plugin at") {
		t.Errorf("missing tree: %v", err)
	}
}

func TestMaterializeToReadRefusesAFIFOWithoutBlocking(t *testing.T) {
	if _, err := exec.LookPath("mkfifo"); err != nil {
		t.Skip("no mkfifo here")
	}
	d := t.TempDir()
	if out, err := exec.Command("mkfifo", filepath.Join(d, "pipe")).CombinedOutput(); err != nil {
		t.Skipf("mkfifo: %v %s", err, out)
	}
	root, err := os.OpenRoot(d)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	done := make(chan error, 1)
	go func() {
		_, _, err := readRooted(root)("pipe")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "pipe isn't a regular file") {
			t.Errorf("err = %v, want a refusal naming it", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO blocked")
	}
}

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, p, c string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTrashFilesSkipsBulk(t *testing.T) {
	dir, trash := t.TempDir(), filepath.Join(t.TempDir(), "trash")
	writeFile(t, filepath.Join(dir, ".env"), "K=1\n")
	writeFile(t, filepath.Join(dir, "node_modules/a/b.js"), "x")
	writeFile(t, filepath.Join(dir, "cfg/local.json"), "{}")
	writeFile(t, filepath.Join(dir, "big.bin"), strings.Repeat("x", trashMaxFile+1))
	names := []string{".env", "node_modules/a/b.js", "cfg/local.json", "big.bin", "sub/"}
	moved, dropped, err := trashFiles(dir, trash, names)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(moved, ",") != ".env,cfg/local.json" {
		t.Errorf("moved %v", moved)
	}
	if strings.Join(dropped, ",") != "node_modules/a/b.js,big.bin,sub/" {
		t.Errorf("dropped %v", dropped)
	}
	if _, err := os.Stat(filepath.Join(trash, "node_modules")); err == nil {
		t.Error("node_modules was copied")
	}
}

func TestTrashFilesCaps(t *testing.T) {
	dir, trash := t.TempDir(), filepath.Join(t.TempDir(), "trash")
	var names []string
	for i := 0; i < trashMaxCount+5; i++ {
		n := "f" + strings.Repeat("a", i%7) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".txt"
		writeFile(t, filepath.Join(dir, n), "x")
		names = append(names, n)
	}
	moved, dropped, err := trashFiles(dir, trash, names)
	if err != nil || len(moved) != trashMaxCount || len(dropped) != 5 {
		t.Errorf("moved %d dropped %d err %v", len(moved), len(dropped), err)
	}
}

func TestNestedRepos(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "inner/.git/HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(dir, "plain/x"), "x")
	got := nestedRepos(dir, []string{"inner/", "plain/", ".env"})
	if len(got) != 1 || got[0] != "inner/" {
		t.Errorf("got %v", got)
	}
}

func TestDirtyItemsWorktreeRename(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@x"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	writeFile(t, filepath.Join(dir, "old.txt"), "some content that is long enough to match\nmore\nlines\n")
	run("add", "old.txt")
	run("commit", "-q", "-m", "x")
	if err := os.Rename(filepath.Join(dir, "old.txt"), filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal(err)
	}
	run("add", "-N", "new.txt")
	mod, unt, err := dirtyItems(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range append(mod, unt...) {
		if n != "new.txt" && n != "old.txt" {
			t.Errorf("stray entry %q in %v %v", n, mod, unt)
		}
	}
	if len(mod)+len(unt) > 2 {
		t.Errorf("entries %v %v", mod, unt)
	}
}

func TestMoveTree(t *testing.T) {
	src := filepath.Join(t.TempDir(), "repo")
	writeFile(t, filepath.Join(src, ".git/HEAD"), "ref\n")
	writeFile(t, filepath.Join(src, "a/b.txt"), "b")
	if err := os.Symlink("/nonexistent", filepath.Join(src, "link")); err != nil {
		t.Skip(err)
	}
	dest := filepath.Join(t.TempDir(), "x", "repo")
	root, err := os.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	err = copyTree(root, dest)
	root.Close()
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "a/b.txt")); err != nil || string(b) != "b" {
		t.Errorf("copied %q %v", b, err)
	}
	if l, err := os.Readlink(filepath.Join(dest, "link")); err != nil || l != "/nonexistent" {
		t.Errorf("link %q %v", l, err)
	}
	if err := moveTree(filepath.Dir(src), "repo/", dest+"2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest+"2", "a/b.txt")); err != nil {
		t.Error(err)
	}
	if _, err := os.Lstat(src); err == nil {
		t.Error("src still there")
	}
	if got := uniqueDest(dest); got != dest+".1" {
		t.Errorf("uniqueDest %q", got)
	}
}

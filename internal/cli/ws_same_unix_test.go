//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSameRegularFile(t *testing.T) {
	dir := t.TempDir()
	w := func(n, c string) string {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	big := make([]byte, 100<<10)
	bigp := filepath.Join(dir, "big")
	os.WriteFile(bigp, big, 0o644)
	big[len(big)-1] = 1
	bigq := filepath.Join(dir, "big2")
	os.WriteFile(bigq, big, 0o644)
	a, b, c := w("a", "KEY=1\n"), w("b", "KEY=1\n"), w("c", "KEY=2\n")
	if !sameRegularFile(a, b) || sameRegularFile(a, c) || sameRegularFile(bigp, bigq) || !sameRegularFile(bigp, bigp) {
		t.Error("content comparison wrong")
	}
	link := filepath.Join(dir, "link")
	os.Symlink(a, link)
	if sameRegularFile(a, link) {
		t.Error("a symlink counts as the same file")
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan bool, 1)
	go func() { done <- sameRegularFile(a, fifo) }()
	select {
	case same := <-done:
		if same {
			t.Error("a FIFO counts as the same file")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("sameRegularFile blocked on a FIFO")
	}
}

func TestTrashFilesNoFollowNoBlock(t *testing.T) {
	dir, trash := t.TempDir(), filepath.Join(t.TempDir(), "trash")
	secret := filepath.Join(t.TempDir(), "id_ed25519")
	os.WriteFile(secret, []byte("PRIVATE"), 0o600)
	if err := os.Symlink(secret, filepath.Join(dir, "link.env")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo.env"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		moved, dropped, err := trashFiles(dir, trash, []string{"link.env", "fifo.env"})
		if err != nil || len(moved) != 0 || len(dropped) != 2 {
			t.Errorf("moved %v dropped %v err %v", moved, dropped, err)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("trashFiles blocked on a FIFO")
	}
	if _, err := os.Stat(trash); err == nil {
		t.Error("something was copied")
	}
}

func TestCopyTreeNoFollowNoBlock(t *testing.T) {
	src := t.TempDir()
	secret := filepath.Join(t.TempDir(), "id_ed25519")
	os.WriteFile(secret, []byte("PRIVATE"), 0o600)
	if err := os.Symlink(secret, filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	// a regular file swapped for a symlink out of the tree can't be opened
	if f, err := openRegularIn(root, "link"); err == nil {
		f.Close()
		t.Error("opened a symlink that leads out of the tree")
	}
	dest := filepath.Join(t.TempDir(), "copy")
	if err := copyTree(root, dest); err != nil {
		t.Fatal(err)
	}
	if l, err := os.Readlink(filepath.Join(dest, "link")); err != nil || l != secret {
		t.Errorf("link %q %v", l, err)
	}
	// a FIFO fails the copy instead of blocking it
	if err := syscall.Mkfifo(filepath.Join(src, "fifo"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- copyTree(root, filepath.Join(t.TempDir(), "copy2")) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("copied a FIFO")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("copyTree blocked on a FIFO")
	}
}

// A parent folder of a listed name swapped for a symlink out of the worktree:
// nothing is copied or moved through it.
func TestParentSymlinkOutOfWorktree(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(outside, ".env"), []byte("HOST"), 0o600)
	if err := os.MkdirAll(filepath.Join(outside, "inner", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "lib")); err != nil {
		t.Fatal(err)
	}
	trash := filepath.Join(t.TempDir(), "trash")
	moved, dropped, err := trashFiles(dir, trash, []string{"lib/.env"})
	if err != nil || len(moved) != 0 || len(dropped) != 1 {
		t.Errorf("moved %v dropped %v err %v", moved, dropped, err)
	}
	if _, err := os.Stat(trash); err == nil {
		t.Error("something was copied")
	}
	dest := filepath.Join(t.TempDir(), "inner")
	if err := moveTree(dir, "lib/inner/", dest); err == nil {
		t.Error("moved a folder through a symlinked parent")
	}
	if _, err := os.Stat(filepath.Join(outside, "inner", ".git")); err != nil {
		t.Errorf("the host folder moved: %v", err)
	}
	if _, err := os.Lstat(dest); err == nil {
		t.Error("something was moved")
	}
	// nor a nested repository that is itself a symlink
	if err := os.Symlink(filepath.Join(outside, "inner"), filepath.Join(dir, "inner")); err != nil {
		t.Fatal(err)
	}
	if err := moveTree(dir, "inner/", dest); err == nil {
		t.Error("moved a symlink as a folder")
	}
}

// A parent folder swapped for a symlink out of the worktree between
// moveTree's check and its rename: nothing outside the worktree moves.
func TestMoveTreeParentSwappedBeforeRename(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	for _, d := range []string{filepath.Join(dir, "lib", "inner", ".git"), filepath.Join(outside, "inner", ".git")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := beforeRename
	t.Cleanup(func() { beforeRename = old })
	beforeRename = func() {
		if err := os.Rename(filepath.Join(dir, "lib"), filepath.Join(dir, "lib.old")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "lib")); err != nil {
			t.Fatal(err)
		}
	}
	dest := filepath.Join(t.TempDir(), "inner")
	if err := moveTree(dir, "lib/inner/", dest); err == nil {
		t.Error("moved a folder through a parent swapped after the check")
	}
	if _, err := os.Stat(filepath.Join(outside, "inner", ".git")); err != nil {
		t.Errorf("the host folder moved: %v", err)
	}
	if _, err := os.Lstat(dest); err == nil {
		t.Error("something was moved")
	}
	// without the swap the same move goes through, as a rename
	beforeRename = func() {}
	if err := os.Remove(filepath.Join(dir, "lib")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "lib.old"), filepath.Join(dir, "lib")); err != nil {
		t.Fatal(err)
	}
	if err := moveTree(dir, "lib/inner/", dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		t.Errorf("not moved: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "lib", "inner")); err == nil {
		t.Error("the source is still there")
	}
}

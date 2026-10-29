package gitx

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Loose refs and reflogs of a submodule are many folders; they don't count
// against the walk's bound.
func TestModuleConfigsSkipsRefFolders(t *testing.T) {
	modules := t.TempDir()
	sub := filepath.Join(modules, "a")
	write(t, filepath.Join(sub, "HEAD"), "ref: refs/heads/main\n")
	os.MkdirAll(filepath.Join(sub, "objects"), 0o755)
	// what a cloned submodule really has: HEAD files in its reflogs and refs
	write(t, filepath.Join(sub, "logs", "HEAD"), "0 1 x\n")
	write(t, filepath.Join(sub, "refs", "remotes", "origin", "HEAD"), "ref: refs/remotes/origin/main\n")
	for _, root := range []string{"refs", "logs"} {
		for i := 0; i < maxModuleDirs+500; i++ {
			os.MkdirAll(filepath.Join(sub, root, "remotes", "origin", fmt.Sprintf("b%d", i)), 0o755)
		}
	}
	files, err := moduleConfigs(modules, 0, new(int))
	if err != nil {
		t.Fatalf("many ref folders: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("files = %v, want the submodule's two config files", files)
	}
}

func gitDirAt(t *testing.T, d string) {
	t.Helper()
	write(t, filepath.Join(d, "HEAD"), "ref: refs/heads/main\n")
	os.MkdirAll(filepath.Join(d, "objects"), 0o755)
}

// A symlink under a refs/ folder refuses: git would follow it.
func TestModuleConfigsRefusesRefSymlink(t *testing.T) {
	modules := t.TempDir()
	gitDirAt(t, filepath.Join(modules, "a"))
	os.MkdirAll(filepath.Join(modules, "a", "refs"), 0o755)
	if err := os.Symlink(t.TempDir(), filepath.Join(modules, "a", "refs", "x")); err != nil {
		t.Skip("no symlinks:", err)
	}
	if _, err := moduleConfigs(modules, 0, new(int)); err == nil {
		t.Fatal("symlink under refs/ not refused")
	}
}

// A git dir deep below refs/ has its config read.
func TestModuleConfigsFindsDeepGitDirUnderRefs(t *testing.T) {
	modules := t.TempDir()
	gitDirAt(t, filepath.Join(modules, "a"))
	gitDirAt(t, filepath.Join(modules, "a", "refs", "1", "2", "3", "4", "5"))
	files, err := moduleConfigs(modules, 0, new(int))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(modules, "a", "refs", "1", "2", "3", "4", "5", "config")
	for _, f := range files {
		if f == want {
			return
		}
	}
	t.Errorf("files = %v, want %s", files, want)
}

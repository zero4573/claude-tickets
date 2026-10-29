package spec

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=x", "-c", "user.email=x@example.com"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git %v: %v: %s", args, err, out)
	}
}

func TestGitTreeBatchAndErrors(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	os.MkdirAll(filepath.Join(dir, "specs", "orders"), 0o755)
	os.MkdirAll(filepath.Join(dir, "specs", ".hidden"), 0o755)
	os.WriteFile(filepath.Join(dir, "specs", "orders", "spec.md"), []byte("one\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "specs", "orders", "extra.md"), []byte("two\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "specs", ".hidden", "spec.md"), []byte("no\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "specs", "big.md"), []byte(strings.Repeat("a", MaxSpecSize+1)), 0o644)
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "base")

	tr, err := GitTree(dir, "HEAD", "specs")
	if err != nil {
		t.Fatal(err)
	}
	if len(tr) != 2 || string(tr["orders/spec.md"]) != "one\n" || string(tr["orders/extra.md"]) != "two\n" {
		t.Fatalf("tree = %v", tr)
	}
	if tr, err := GitTree(dir, "HEAD", "nospecs"); err != nil || len(tr) != 0 {
		t.Errorf("missing folder: %v, %v", tr, err)
	}
	if _, err := GitTree(dir, "no-such-rev", "specs"); err == nil {
		t.Error("an unresolved rev must be an error, not an empty tree")
	}
	if _, err := MergeBase(dir, "no-such-base"); err == nil {
		t.Error("an unresolved base must be an error")
	}
	if mb, err := MergeBase(dir, "main"); err != nil || mb == "" {
		t.Errorf("MergeBase = %q, %v", mb, err)
	}
}

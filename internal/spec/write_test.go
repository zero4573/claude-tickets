package spec

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteTreeIgnoresAndRemovesLeftoverTmp(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "base")
	tmp := filepath.Join(parent, ".base.tmp-abc")
	os.MkdirAll(tmp, 0o755)
	os.WriteFile(filepath.Join(tmp, "junk.md"), []byte("x"), 0o644)
	if err := WriteTree(root, Tree{"a/spec.md": []byte("1")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("tmp left: %v", err)
	}
	got, _ := ReadTree(root)
	if len(got) != 1 || string(got["a/spec.md"]) != "1" {
		t.Fatalf("tree %v", got)
	}
}

func TestWriteTreeReplacesWithoutStaleFiles(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "base")
	WriteTree(root, Tree{"a/spec.md": []byte("1"), "b/spec.md": []byte("2")})
	if err := WriteTree(root, Tree{"a/spec.md": []byte("3")}); err != nil {
		t.Fatal(err)
	}
	got, _ := ReadTree(root)
	if len(got) != 1 || string(got["a/spec.md"]) != "3" {
		t.Fatalf("tree %v", got)
	}
	ents, _ := os.ReadDir(parent)
	if len(ents) != 1 {
		t.Fatalf("leftovers: %v", ents)
	}
}

func TestWriteTreeFailedSecondRenameRestores(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "base")
	WriteTree(root, Tree{"a/spec.md": []byte("old")})
	calls := 0
	rename = func(a, b string) error {
		calls++
		if calls == 2 {
			return errors.New("boom")
		}
		return os.Rename(a, b)
	}
	err := WriteTree(root, Tree{"a/spec.md": []byte("new")})
	rename = os.Rename
	if err == nil {
		t.Fatal("want error")
	}
	if _, e := os.Lstat(root); !os.IsNotExist(e) {
		t.Fatalf("root should be absent mid-crash: %v", e)
	}
	if err := WriteTree(root, Tree{"a/spec.md": []byte("new")}); err != nil {
		t.Fatal(err)
	}
	got, _ := ReadTree(root)
	if string(got["a/spec.md"]) != "new" {
		t.Fatalf("tree %v", got)
	}
	// restore alone: crash, then a write that restores first
	WriteTree(root, Tree{"a/spec.md": []byte("v1")})
	calls = 0
	rename = func(a, b string) error {
		calls++
		if calls == 2 {
			return errors.New("boom")
		}
		return os.Rename(a, b)
	}
	WriteTree(root, Tree{"a/spec.md": []byte("v2")})
	rename = os.Rename
	olds, _ := filepath.Glob(filepath.Join(parent, ".base.old-*"))
	if len(olds) != 1 {
		t.Fatalf("olds %v", olds)
	}
	if err := os.Rename(olds[0], root); err != nil {
		t.Fatal(err)
	}
	got, _ = ReadTree(root)
	if string(got["a/spec.md"]) != "v1" {
		t.Fatalf("restore %v", got)
	}
}

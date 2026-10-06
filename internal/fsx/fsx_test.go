package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWithin(t *testing.T) {
	j := filepath.Join
	for _, tc := range []struct {
		path, dir      string
		within, inside bool
	}{
		{j("a", "b"), "a", true, true},
		{"a", "a", true, false},
		{j("a", "b", ".."), "a", true, false},
		{"ab", "a", false, false},
		{j("a", "..", "c"), "a", false, false},
		{"..a", ".", true, true},
	} {
		if got := Within(tc.path, tc.dir); got != tc.within {
			t.Errorf("Within(%q, %q) = %t", tc.path, tc.dir, got)
		}
		if got := Inside(tc.path, tc.dir); got != tc.inside {
			t.Errorf("Inside(%q, %q) = %t", tc.path, tc.dir, got)
		}
	}
	for rel, want := range map[string]int{".": 0, "a": 1, j("a", "b", "c"): 3, j("a", "b") + string(filepath.Separator): 2} {
		if got := Depth(rel); got != want {
			t.Errorf("Depth(%q) = %d, want %d", rel, got, want)
		}
	}
}

func TestCopyTree(t *testing.T) {
	src, dest := t.TempDir(), filepath.Join(t.TempDir(), "out")
	os.MkdirAll(filepath.Join(src, "cache", "x"), 0o755)
	os.WriteFile(filepath.Join(src, "graph.json"), []byte("{}"), 0o600)
	os.WriteFile(filepath.Join(src, "cache", "x", "a"), []byte("a"), 0o644)
	if err := CopyTree(src, dest); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dest, "cache", "x", "a")); string(data) != "a" {
		t.Error("nested file not copied")
	}
	if st, _ := os.Stat(filepath.Join(dest, "graph.json")); st == nil || st.Mode().Perm() != 0o600 {
		t.Error("mode not kept")
	}
	if err := RemoveAll(dest); err != nil || IsDir(dest) {
		t.Error("not removed", err)
	}
}

func TestRemoveEmptyDirs(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "a", "b"), 0o755)
	os.MkdirAll(filepath.Join(d, "c"), 0o755)
	os.WriteFile(filepath.Join(d, "c", "f"), []byte("x"), 0o644)
	RemoveEmptyDirs(d)
	if IsDir(filepath.Join(d, "a")) || !IsDir(filepath.Join(d, "c")) {
		t.Error("empty dirs not removed, or a non-empty one was")
	}
}

func TestWriteFileAtomic(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "n.md")
	os.WriteFile(p, []byte("old"), 0o600)
	if err := WriteFileAtomic(p, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); string(data) != "new" {
		t.Errorf("content = %q", data)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want the old file's", st.Mode().Perm())
	}
	if entries, _ := os.ReadDir(d); len(entries) != 1 {
		t.Errorf("temporary file left behind: %v", entries)
	}
	q := filepath.Join(d, "new.md")
	if err := WriteFileAtomic(q, []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(q); st.Mode().Perm() != 0o640 {
		t.Errorf("new file mode = %v", st.Mode().Perm())
	}
}

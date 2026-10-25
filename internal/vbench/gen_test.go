package vbench

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	va, err := Generate(a, 7)
	if err != nil {
		t.Fatal(err)
	}
	vb, err := Generate(b, 7)
	if err != nil {
		t.Fatal(err)
	}
	if va.Notes < 2000 || va.Notes != vb.Notes {
		t.Fatalf("notes = %d / %d", va.Notes, vb.Notes)
	}
	if len(va.Facts) != 20 {
		t.Fatalf("facts = %d", len(va.Facts))
	}
	for _, f := range va.Facts {
		ba, err := os.ReadFile(filepath.Join(a, filepath.FromSlash(f.Path)))
		if err != nil || !strings.Contains(string(ba), "## "+f.Section+"\n\n"+f.Sentence+"\n") {
			t.Fatalf("fact %q not planted in %s: %v", f.Question, f.Path, err)
		}
		bb, _ := os.ReadFile(filepath.Join(b, filepath.FromSlash(f.Path)))
		if !bytes.Equal(ba, bb) {
			t.Fatalf("%s differs between runs of the same seed", f.Path)
		}
	}
	files := mdFiles(a)
	if len(files) != va.Notes {
		t.Fatalf("on disk %d, generated %d", len(files), va.Notes)
	}
	// a different seed differs
	c := t.TempDir()
	if _, err := Generate(c, 8); err != nil {
		t.Fatal(err)
	}
	x, _ := os.ReadFile(filepath.Join(a, "knowledge-base", "kb-001.md"))
	y, _ := os.ReadFile(filepath.Join(c, "knowledge-base", "kb-001.md"))
	if bytes.Equal(x, y) {
		t.Fatal("seed ignored")
	}
}

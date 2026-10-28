package graph

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGraphOutWorktreeOut(t *testing.T) {
	got := WorktreeOut(filepath.Join("w", "MAN-1"), "github-acme-billing")
	want := filepath.Join("w", "MAN-1", ".cache", "graphify", "github-acme-billing", "graphify-out")
	if got != want {
		t.Fatalf("WorktreeOut = %q, want %q", got, want)
	}
}

func TestGraphOutSeed(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	src := filepath.Join(main, "graphify-out")
	if err := os.MkdirAll(filepath.Join(src, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"graph.json", "manifest.json", "cache/a", "other.html"} {
		if err := os.WriteFile(filepath.Join(src, f), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dest := WorktreeOut(filepath.Join(root, "ws"), "slug")
	Seed(main, dest)
	for _, f := range []string{"graph.json", "manifest.json", "cache/a"} {
		if _, err := os.Stat(filepath.Join(dest, f)); err != nil {
			t.Errorf("%s not seeded: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "other.html")); err == nil {
		t.Error("other.html was copied")
	}
	// an existing destination is left alone
	if err := os.WriteFile(filepath.Join(dest, "graph.json"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	Seed(main, dest)
	if b, _ := os.ReadFile(filepath.Join(dest, "graph.json")); string(b) != "mine" {
		t.Errorf("seed overwrote an existing graph: %q", b)
	}
	// no source graph: nothing created
	none := filepath.Join(root, "none")
	Seed(filepath.Join(root, "nomain"), none)
	if _, err := os.Stat(none); err == nil {
		t.Error("Seed created a destination without a source")
	}
}

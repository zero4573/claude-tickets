package workspace

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateRefusesRunning(t *testing.T) {
	root := t.TempDir()
	oldDir, newDir := filepath.Join(root, ".kb-a"), filepath.Join(root, ".spec-a")
	if err := Ensure(oldDir, "KB", "/v"); err != nil {
		t.Fatal(err)
	}
	oldC := inContainer
	inContainer = func() bool { return false }
	t.Cleanup(func() { inContainer = oldC })
	if err := RecordSession(oldDir, "kb"); err != nil {
		t.Fatal(err)
	}
	moved, err := Migrate(oldDir, newDir)
	if err == nil || moved || !strings.Contains(err.Error(), "close it first") {
		t.Fatalf("Migrate = %v, %v", moved, err)
	}
	if _, err := os.Stat(oldDir); err != nil {
		t.Error("oldDir moved")
	}
}

func TestMigrateSymlinkedRoot(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks")
	}
	oldDir, newDir := filepath.Join(link, ".kb-a"), filepath.Join(link, ".spec-a")
	if err := Ensure(oldDir, "KB", "/v"); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(real, ".kb-a", "slug")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	// recorded through the resolved spelling, workspace passed via the link
	if err := Put(File(oldDir), Repo{Slug: "slug", Path: clone, Mode: "explore"}); err != nil {
		t.Fatal(err)
	}
	if moved, err := Migrate(oldDir, newDir); err != nil || !moved {
		t.Fatalf("Migrate = %v, %v", moved, err)
	}
	info, _ := Read(newDir)
	if len(info.Repos) != 1 || info.Repos[0].Path != filepath.Join(newDir, "slug") {
		t.Errorf("repo path not rewritten: %+v", info.Repos)
	}
}

func TestMigrateWarnsOnLeftover(t *testing.T) {
	root := t.TempDir()
	oldDir, newDir := filepath.Join(root, ".kb-a"), filepath.Join(root, ".spec-a")
	for _, d := range []string{oldDir, newDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	warnOut = &buf
	defer func() { warnOut = os.Stderr }()
	if moved, err := Migrate(oldDir, newDir); err != nil || moved {
		t.Fatalf("Migrate = %v, %v", moved, err)
	}
	if !strings.Contains(buf.String(), oldDir) {
		t.Errorf("no warning naming %s: %q", oldDir, buf.String())
	}
}

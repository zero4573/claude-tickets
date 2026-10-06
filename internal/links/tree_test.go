package links

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func exists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, rel))
	return err == nil
}

func treeVault(t *testing.T) string {
	v := t.TempDir()
	write(t, v, ".obsidian/app.json", "{}")
	write(t, v, "tickets/MAN-4/MAN-4.md", "[[MAN-4/requirements]] ![[logs/x]]\n")
	write(t, v, "tickets/MAN-4/requirements.md", "")
	write(t, v, "tickets/MAN-4/logs/x.md", "![[shot.png]]\n")
	write(t, v, "tickets/MAN-4/shot.png", "")
	write(t, v, "tickets/MAN-5/requirements.md", "")
	write(t, v, "projects/p/p.md", "[[MAN-4]] [[tickets/MAN-4/logs/x|log]] [[requirements]] `[[tickets/MAN-4/logs/x]]`\n")
	return v
}

func TestMoveTreeAll(t *testing.T) {
	v := treeVault(t)
	moved, n, err := MoveTree(v, filepath.Join(v, "tickets/MAN-4"), filepath.Join(v, "archive/tickets/MAN-4"), nil, "MAN-4.md")
	if err != nil {
		t.Fatal(err)
	}
	if moved != 4 || n != 3 {
		t.Errorf("moved %d, rewrote %d", moved, n)
	}
	if exists(v, "tickets/MAN-4") || !exists(v, "archive/tickets/MAN-4/MAN-4.md") || !exists(v, "archive/tickets/MAN-4/logs/x.md") {
		t.Error("folder not moved, or not removed")
	}
	// The duplicate requirements.md elsewhere didn't block it; bare links stay
	if got := read(t, v, "projects/p/p.md"); got != "[[MAN-4]] [[archive/tickets/MAN-4/logs/x|log]] [[requirements]] `[[tickets/MAN-4/logs/x]]`\n" {
		t.Errorf("p.md: %q", got)
	}
	if got := read(t, v, "archive/tickets/MAN-4/MAN-4.md"); got != "[[archive/tickets/MAN-4/requirements]] ![[archive/tickets/MAN-4/logs/x]]\n" {
		t.Errorf("MAN-4.md: %q", got)
	}
	// Again: nothing left to move
	if moved, _, err := MoveTree(v, filepath.Join(v, "tickets/MAN-4"), filepath.Join(v, "archive/tickets/MAN-4"), nil, "MAN-4.md"); err != nil || moved != 0 {
		t.Errorf("re-run: %d, %v", moved, err)
	}
}

func TestMoveTreeSome(t *testing.T) {
	v := treeVault(t)
	src, dst := filepath.Join(v, "tickets/MAN-4"), filepath.Join(v, "archive/tickets/MAN-4")
	moved, _, err := MoveTree(v, src, dst, []string{filepath.Join(src, "logs/x.md"), filepath.Join(src, "shot.png")}, "")
	if err != nil || moved != 2 {
		t.Fatal(moved, err)
	}
	if !exists(v, "tickets/MAN-4/MAN-4.md") || exists(v, "tickets/MAN-4/logs") || !exists(v, "archive/tickets/MAN-4/shot.png") {
		t.Error("wrong files moved, or empty logs/ left")
	}
}

func TestMoveTreeDestinationExists(t *testing.T) {
	v := treeVault(t)
	write(t, v, "archive/tickets/MAN-4/requirements.md", "")
	before := read(t, v, "projects/p/p.md")
	if _, _, err := MoveTree(v, filepath.Join(v, "tickets/MAN-4"), filepath.Join(v, "archive/tickets/MAN-4"), nil, ""); err == nil ||
		!strings.Contains(err.Error(), "destination exists: archive/tickets/MAN-4/requirements.md") {
		t.Errorf("err = %v", err)
	}
	if !exists(v, "tickets/MAN-4/logs/x.md") || read(t, v, "projects/p/p.md") != before {
		t.Error("changed something before failing")
	}
	if _, _, err := MoveTree(v, filepath.Join(v, "tickets/MAN-4"), filepath.Join(v, "tickets/MAN-4/sub"), nil, ""); err == nil {
		t.Error("a destination inside the source")
	}
}

func TestMoveTreeResumes(t *testing.T) {
	v := treeVault(t)
	src, dst := filepath.Join(v, "tickets/MAN-4"), filepath.Join(v, "archive/tickets/MAN-4")
	// A run that rewrote the links and moved only shot.png before it stopped
	write(t, v, "projects/p/p.md", "[[archive/tickets/MAN-4/logs/x|log]]\n")
	os.MkdirAll(dst, 0o755)
	os.Rename(filepath.Join(src, "shot.png"), filepath.Join(dst, "shot.png"))
	moved, _, err := MoveTree(v, src, dst, []string{}, "")
	if err != nil || moved != 1 || !exists(v, "archive/tickets/MAN-4/logs/x.md") || !exists(v, "tickets/MAN-4/MAN-4.md") {
		t.Errorf("resume: %d, %v", moved, err)
	}
}

func TestMoveTreeLast(t *testing.T) {
	v := treeVault(t)
	src := filepath.Join(v, "tickets/MAN-4")
	// A file that can't be moved (its folder is read-only) stops the move
	// before the last one goes
	os.Chmod(filepath.Join(src, "logs"), 0o555)
	defer os.Chmod(filepath.Join(src, "logs"), 0o755)
	if os.WriteFile(filepath.Join(src, "logs", "probe"), nil, 0o644) == nil {
		t.Skip("running as root: permissions aren't enforced")
	}
	if _, _, err := MoveTree(v, src, filepath.Join(v, "archive/tickets/MAN-4"), nil, "MAN-4.md"); err == nil {
		t.Fatal("no error")
	}
	if !exists(v, "tickets/MAN-4/MAN-4.md") {
		t.Error("the last file moved although another failed")
	}
}

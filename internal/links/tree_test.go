package links

import (
	"os"
	"path/filepath"
	"reflect"
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
	res, err := MoveTree(v, filepath.Join(v, "tickets/MAN-4"), filepath.Join(v, "archive/tickets/MAN-4"), nil, "MAN-4.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Moves) != 4 || res.Rewritten != 3 {
		t.Errorf("moved %d, rewrote %d", len(res.Moves), res.Rewritten)
	}
	if last := res.Moves[3]; last.From != "tickets/MAN-4/MAN-4.md" || last.To != "archive/tickets/MAN-4/MAN-4.md" {
		t.Errorf("the note should move last: %+v", res.Moves)
	}
	if want := []string{"projects/p/p.md", "tickets/MAN-4/MAN-4.md"}; !reflect.DeepEqual(res.Changed, want) {
		t.Errorf("Changed = %v, want %v", res.Changed, want)
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
	if res, err := MoveTree(v, filepath.Join(v, "tickets/MAN-4"), filepath.Join(v, "archive/tickets/MAN-4"), nil, "MAN-4.md"); err != nil || len(res.Moves) != 0 || res.Changed != nil {
		t.Errorf("re-run: %+v, %v", res, err)
	}
}

func TestMoveTreeSome(t *testing.T) {
	v := treeVault(t)
	src, dst := filepath.Join(v, "tickets/MAN-4"), filepath.Join(v, "archive/tickets/MAN-4")
	res, err := MoveTree(v, src, dst, []string{filepath.Join(src, "logs/x.md"), filepath.Join(src, "shot.png")}, "")
	if err != nil || len(res.Moves) != 2 {
		t.Fatal(res, err)
	}
	if !exists(v, "tickets/MAN-4/MAN-4.md") || exists(v, "tickets/MAN-4/logs") || !exists(v, "archive/tickets/MAN-4/shot.png") {
		t.Error("wrong files moved, or empty logs/ left")
	}
}

func TestMoveTreeDestinationExists(t *testing.T) {
	v := treeVault(t)
	write(t, v, "archive/tickets/MAN-4/requirements.md", "")
	before := read(t, v, "projects/p/p.md")
	if _, err := MoveTree(v, filepath.Join(v, "tickets/MAN-4"), filepath.Join(v, "archive/tickets/MAN-4"), nil, ""); err == nil ||
		!strings.Contains(err.Error(), "destination exists: archive/tickets/MAN-4/requirements.md") {
		t.Errorf("err = %v", err)
	}
	if !exists(v, "tickets/MAN-4/logs/x.md") || read(t, v, "projects/p/p.md") != before {
		t.Error("changed something before failing")
	}
	if _, err := MoveTree(v, filepath.Join(v, "tickets/MAN-4"), filepath.Join(v, "tickets/MAN-4/sub"), nil, ""); err == nil {
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
	res, err := MoveTree(v, src, dst, []string{}, "")
	moved := len(res.Moves)
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
	if _, err := MoveTree(v, src, filepath.Join(v, "archive/tickets/MAN-4"), nil, "MAN-4.md"); err == nil {
		t.Fatal("no error")
	}
	if !exists(v, "tickets/MAN-4/MAN-4.md") {
		t.Error("the last file moved although another failed")
	}
}

func TestMoveTreePartialResultAfterWriteError(t *testing.T) {
	skipIfCanWriteReadOnly(t)
	v := t.TempDir()
	write(t, v, ".obsidian/app.json", "{}")
	write(t, v, "tickets/T/T.md", "")
	write(t, v, "tickets/T/r.md", "")
	write(t, v, "a/ok.md", "[[T/r]]\n")
	write(t, v, "b/ro/stuck.md", "[[T/r]]\n")
	ro := filepath.Join(v, "b/ro")
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(ro, 0o755)
	res, err := MoveTree(v, filepath.Join(v, "tickets/T"), filepath.Join(v, "archive/T"), nil, "T.md")
	if err == nil {
		t.Fatal("expected a write error")
	}
	if len(res.Moves) != 0 {
		t.Errorf("Moves = %v, want none", res.Moves)
	}
	if want := []string{"a/ok.md"}; !reflect.DeepEqual(res.Changed, want) {
		t.Errorf("Changed = %v, want %v", res.Changed, want)
	}
}

package links

import (
	"errors"
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
	// A file that can't be moved stops the move before the last one goes
	orig := moveFile
	t.Cleanup(func() { moveFile = orig })
	moveFile = func(from, to string) error {
		if strings.HasSuffix(from, "x.md") {
			return errors.New("injected move error")
		}
		return orig(from, to)
	}
	if _, err := MoveTree(v, src, filepath.Join(v, "archive/tickets/MAN-4"), nil, "MAN-4.md"); err == nil {
		t.Fatal("no error")
	}
	if !exists(v, "tickets/MAN-4/MAN-4.md") {
		t.Error("the last file moved although another failed")
	}
}

func TestMoveTreePartialResultAfterWriteError(t *testing.T) {
	v := t.TempDir()
	write(t, v, ".obsidian/app.json", "{}")
	write(t, v, "tickets/T/T.md", "")
	write(t, v, "tickets/T/r.md", "")
	write(t, v, "a/ok.md", "[[T/r]]\n")
	write(t, v, "b/ro/stuck.md", "[[T/r]]\n")
	failWriteOn(t, v, "b/ro/stuck.md")
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

func TestMoveTrees(t *testing.T) {
	v := treeVault(t)
	trees := []Tree{
		{Src: filepath.Join(v, "tickets/MAN-4"), Dst: filepath.Join(v, "tickets/manual/MAN-4"), Last: "MAN-4.md"},
		{Src: filepath.Join(v, "tickets/MAN-5"), Dst: filepath.Join(v, "tickets/manual/MAN-5")},
	}
	write(t, v, "tickets/manual/MAN-5/requirements.md", "taken")
	before := read(t, v, "projects/p/p.md")
	if _, err := MoveTrees(v, trees); err == nil || !strings.Contains(err.Error(), "destination exists: tickets/manual/MAN-5/requirements.md") {
		t.Errorf("err = %v", err)
	}
	if !exists(v, "tickets/MAN-4/MAN-4.md") || read(t, v, "projects/p/p.md") != before {
		t.Error("changed something before failing")
	}
	os.Remove(filepath.Join(v, "tickets/manual/MAN-5/requirements.md"))
	res, err := MoveTrees(v, trees)
	if err != nil {
		t.Fatal(err)
	}
	if last := res.Moves[len(res.Moves)-1]; last.To != "tickets/manual/MAN-4/MAN-4.md" {
		t.Errorf("last move %v", last)
	}
	if got := read(t, v, "projects/p/p.md"); got != "[[MAN-4]] [[tickets/manual/MAN-4/logs/x|log]] [[requirements]] `[[tickets/MAN-4/logs/x]]`\n" {
		t.Errorf("p.md = %q", got)
	}
	if exists(v, "tickets/MAN-4") || exists(v, "tickets/MAN-5") || !exists(v, "tickets/manual/MAN-5/requirements.md") {
		t.Error("folders not moved")
	}
	if _, err := MoveTrees(v, []Tree{{Src: filepath.Join(v, "tickets/manual"), Dst: filepath.Join(v, "tickets/manual/x")}}); err == nil {
		t.Error("a destination inside the source")
	}
}

func renameVault(t *testing.T) string {
	v := t.TempDir()
	write(t, v, "tickets/manual/MAN-3/MAN-3.md", "see [[MAN-3-01-x]]\n")
	write(t, v, "tickets/manual/MAN-3/MAN-3-01-x.md", "back [[MAN-3]]\n")
	write(t, v, "tickets/manual/MAN-3/logs/s.md", "")
	write(t, v, "projects/p.md", "[[MAN-3]] [[tickets/manual/MAN-3/MAN-3-01-x|alias]] [[MAN-3#Goal]] [[tickets/manual/MAN-3/logs/s]] [[MAN-3-01-x#H|a]]\n")
	return v
}

func renameMan(b string) string { return strings.Replace(b, "MAN-3", "ABC-9", 1) }

const renamedP = "[[ABC-9]] [[tickets/jira/ABC-9/ABC-9-01-x|alias]] [[ABC-9#Goal]] [[tickets/jira/ABC-9/logs/s]] [[ABC-9-01-x#H|a]]\n"

func TestRenameTree(t *testing.T) {
	v := renameVault(t)
	if err := RenameTree(v, filepath.Join(v, "tickets/manual/MAN-3"), filepath.Join(v, "tickets/jira/ABC-9"), renameMan, ""); err != nil {
		t.Fatal(err)
	}
	if exists(v, "tickets/manual/MAN-3") || !exists(v, "tickets/jira/ABC-9/ABC-9.md") || !exists(v, "tickets/jira/ABC-9/ABC-9-01-x.md") || !exists(v, "tickets/jira/ABC-9/logs/s.md") {
		t.Error("tree not moved and renamed")
	}
	if got := read(t, v, "projects/p.md"); got != renamedP {
		t.Errorf("p.md: %q", got)
	}
	if got := read(t, v, "tickets/jira/ABC-9/ABC-9.md"); got != "see [[ABC-9-01-x]]\n" {
		t.Errorf("note: %q", got)
	}
	if got := read(t, v, "tickets/jira/ABC-9/ABC-9-01-x.md"); got != "back [[ABC-9]]\n" {
		t.Errorf("task: %q", got)
	}
	if n := len(Problems(v, nil)); n != 0 {
		t.Errorf("%d broken links", n)
	}
}

func TestRenameTreeDestinationExists(t *testing.T) {
	v := renameVault(t)
	write(t, v, "tickets/jira/ABC-9/ABC-9.md", "")
	err := RenameTree(v, filepath.Join(v, "tickets/manual/MAN-3"), filepath.Join(v, "tickets/jira/ABC-9"), renameMan, "")
	if err == nil || !strings.Contains(err.Error(), "destination exists") {
		t.Fatalf("err = %v", err)
	}
	if !exists(v, "tickets/manual/MAN-3/MAN-3.md") || read(t, v, "projects/p.md") == renamedP {
		t.Error("changed despite the refusal")
	}
}

func TestRenameTreeRerunAfterFailure(t *testing.T) {
	v := renameVault(t)
	n := 0
	orig := moveFile
	moveFile = func(s, d string) error {
		if n++; n == 2 {
			return errors.New("injected")
		}
		return orig(s, d)
	}
	src, dst := filepath.Join(v, "tickets/manual/MAN-3"), filepath.Join(v, "tickets/jira/ABC-9")
	err := RenameTree(v, src, dst, renameMan, "")
	moveFile = orig
	if err == nil {
		t.Fatal("want the injected failure")
	}
	if err := RenameTree(v, src, dst, renameMan, ""); err != nil {
		t.Fatal(err)
	}
	if exists(v, "tickets/manual/MAN-3") || !exists(v, "tickets/jira/ABC-9/ABC-9.md") || !exists(v, "tickets/jira/ABC-9/ABC-9-01-x.md") || !exists(v, "tickets/jira/ABC-9/logs/s.md") {
		t.Error("not finished")
	}
	if got := read(t, v, "projects/p.md"); got != renamedP {
		t.Errorf("p.md: %q", got)
	}
	if n := len(Problems(v, nil)); n != 0 {
		t.Errorf("%d broken links", n)
	}
}

func TestRenameTreeLastMovesNoteLast(t *testing.T) {
	v := renameVault(t)
	var moved []string
	orig := moveFile
	moveFile = func(s, d string) error {
		moved = append(moved, filepath.Base(s))
		if len(moved) == 2 {
			return errors.New("injected")
		}
		return orig(s, d)
	}
	src, dst := filepath.Join(v, "tickets/manual/MAN-3"), filepath.Join(v, "tickets/jira/ABC-9")
	err := RenameTree(v, src, dst, renameMan, "MAN-3.md")
	moveFile = orig
	if err == nil {
		t.Fatal("want the injected failure")
	}
	if !exists(v, "tickets/manual/MAN-3/MAN-3.md") {
		t.Errorf("the note moved before the other files (moves: %v)", moved)
	}
	if err := RenameTree(v, src, dst, renameMan, "MAN-3.md"); err != nil {
		t.Fatal(err)
	}
	if exists(v, "tickets/manual/MAN-3") || !exists(v, "tickets/jira/ABC-9/ABC-9.md") {
		t.Error("not finished")
	}
}

func TestRedirectLinks(t *testing.T) {
	v := treeVault(t)
	rs := []Redirect{{Src: "tickets/MAN-4/logs/x.md", Dst: "archive/x.md"}}
	got, n := RewriteText(filepath.Join(v, "a.md"), "[[tickets/MAN-4/logs/x|log]] [[x]] `[[tickets/MAN-4/logs/x]]`", rs)
	if n != 1 || got != "[[archive/x|log]] [[x]] `[[tickets/MAN-4/logs/x]]`" {
		t.Errorf("RewriteText: %d %q", n, got)
	}
	res, err := RedirectLinks(v, rs)
	if err != nil || res.Rewritten != 2 || len(res.Moves) != 0 {
		t.Fatalf("RedirectLinks: %+v %v", res, err)
	}
	if want := []string{"projects/p/p.md", "tickets/MAN-4/MAN-4.md"}; !reflect.DeepEqual(res.Changed, want) {
		t.Errorf("Changed = %v, want %v", res.Changed, want)
	}
	if !exists(v, "tickets/MAN-4/logs/x.md") || exists(v, "archive/x.md") {
		t.Error("RedirectLinks moved a file")
	}
	if got := read(t, v, "projects/p/p.md"); got != "[[MAN-4]] [[archive/x|log]] [[requirements]] `[[tickets/MAN-4/logs/x]]`\n" {
		t.Errorf("p.md: %q", got)
	}
	// a rerun finds nothing left to rewrite
	if res, err := RedirectLinks(v, rs); err != nil || res.Rewritten != 0 || res.Changed != nil {
		t.Errorf("rerun: %+v %v", res, err)
	}
}

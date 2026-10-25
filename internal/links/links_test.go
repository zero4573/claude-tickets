package links

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func vault(t *testing.T) string {
	v := t.TempDir()
	write(t, v, ".obsidian/app.json", "{}")
	write(t, v, "projects/x/sequences/flow.md", "# flow\n")
	write(t, v, "projects/x/x.md", "see [[flow]] and [[sequences/flow#Steps|the flow]] and ![[diagram.png]]\n"+
		"`[[not-a-link]]` inline, [[#local]]\n```\n[[in-code]]\n```\n[[missing]]\n")
	write(t, v, "projects/x/diagram.png", "png")
	write(t, v, "knowledge-base/dup.md", "")
	write(t, v, "references/dup.md", "")
	write(t, v, "inbox/a.md", "[[dup]]\n")
	write(t, v, "templates/t.md", "[[nowhere]]\n")
	write(t, v, ".trash/old.md", "[[nowhere]]\n")
	return v
}

func TestCheck(t *testing.T) {
	v := vault(t)
	var out strings.Builder
	n := Check(v, nil, &out)
	got := out.String()
	if n != 2 {
		t.Fatalf("problems = %d, want 2:\n%s", n, got)
	}
	for _, want := range []string{
		"projects/x/x.md:6: unresolved link [[missing]]",
		"inbox/a.md:1: ambiguous link [[dup]] -> knowledge-base/dup.md, references/dup.md",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	out.Reset()
	if n := Check(v, []string{filepath.Join(v, "projects/x/sequences/flow.md")}, &out); n != 0 || !strings.Contains(out.String(), "1 file(s) checked, all links resolve") {
		t.Errorf("clean file: %d %s", n, out.String())
	}
}

func TestMove(t *testing.T) {
	v := vault(t)
	os.MkdirAll(filepath.Join(v, "projects/x/features"), 0o755)
	res, err := Move(v, "projects/x/sequences/flow.md", "projects/x/features")
	if err != nil {
		t.Fatal(err)
	}
	if want := (MoveResult{Moves: []Moved{{"projects/x/sequences/flow.md", "projects/x/features/flow.md"}}, Rewritten: 1, Changed: []string{"projects/x/x.md"}}); !reflect.DeepEqual(res, want) {
		t.Errorf("Move = %+v, want %+v", res, want)
	}
	data, _ := os.ReadFile(filepath.Join(v, "projects/x/x.md"))
	if s := string(data); !strings.Contains(s, "[[flow]] and [[projects/x/features/flow#Steps|the flow]]") || !strings.Contains(s, "`[[not-a-link]]`") {
		t.Errorf("rewritten note:\n%s", s)
	}
	if _, err := Move(v, "inbox/a.md", "knowledge-base/x.md"); err == nil || !strings.Contains(err.Error(), "already used by projects/x/x.md") {
		t.Errorf("clash: %v", err)
	}
	if _, err := Move(v, "inbox/a.md", "references/dup.md"); err == nil || !strings.Contains(err.Error(), "destination exists") {
		t.Errorf("exists: %v", err)
	}
	if _, err := Move(v, "inbox/a.md", "/tmp/a.md"); err == nil || !strings.Contains(err.Error(), "inside the vault") {
		t.Errorf("outside: %v", err)
	}
}

func TestMoveRename(t *testing.T) {
	v := vault(t)
	write(t, v, "inbox/b.md", "[[x]] and [[X#Top|the x]] and `[[x]]`\n")
	res, err := Move(v, "projects/x/x.md", "projects/x/x-overview.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := (MoveResult{Moves: []Moved{{"projects/x/x.md", "projects/x/x-overview.md"}}, Rewritten: 2, Changed: []string{"inbox/b.md"}}); !reflect.DeepEqual(res, want) {
		t.Errorf("Move = %+v, want %+v", res, want)
	}
	if data, _ := os.ReadFile(filepath.Join(v, "inbox/b.md")); string(data) != "[[x-overview]] and [[x-overview#Top|the x]] and `[[x]]`\n" {
		t.Errorf("bare links not renamed:\n%s", data)
	}
	// dup.md exists twice: [[dup]] named the other one too, so it stays
	if _, err := Move(v, "references/dup.md", "references/dup-ref.md"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(v, "inbox/a.md")); string(data) != "[[dup]]\n" {
		t.Errorf("an ambiguous bare link was rewritten: %s", data)
	}
}

// A note that links to itself by path: it is rewritten after the move, so
// Changed names its new path; a refusal returns the zero result.
func TestMoveSelfLink(t *testing.T) {
	v := vault(t)
	write(t, v, "projects/x/sequences/flow.md", "# flow\n[[sequences/flow]]\n")
	res, err := Move(v, "projects/x/sequences/flow.md", "projects/x/f2.md")
	if err != nil {
		t.Fatal(err)
	}
	want := MoveResult{Moves: []Moved{{"projects/x/sequences/flow.md", "projects/x/f2.md"}}, Rewritten: 3, Changed: []string{"projects/x/f2.md", "projects/x/x.md"}}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("Move = %+v, want %+v", res, want)
	}
	if res, err := Move(v, "inbox/a.md", "references/dup.md"); err == nil || !reflect.DeepEqual(res, MoveResult{}) {
		t.Errorf("refusal: %+v, %v", res, err)
	}
}

// failWriteOn makes writeNote fail for the note at vault-relative rel only.
func failWriteOn(t *testing.T, v, rel string) {
	t.Helper()
	orig := writeNote
	t.Cleanup(func() { writeNote = orig })
	bad := filepath.Join(v, rel)
	writeNote = func(f string, data []byte) error {
		if f == bad {
			return errors.New("injected write error")
		}
		return orig(f, data)
	}
}

func TestMoveKeepsSymlinkAndMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	v := t.TempDir()
	write(t, v, ".obsidian/app.json", "{}")
	write(t, v, "inbox/x.md", "# x\n")
	write(t, v, "real/target.md", "[[inbox/x]]\n")
	write(t, v, "a/private.md", "[[inbox/x]]\n")
	if err := os.Symlink(filepath.Join(v, "real/target.md"), filepath.Join(v, "a/link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(v, "a/private.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Move(v, "inbox/x.md", "notes/x.md"); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Lstat(filepath.Join(v, "a/link.md")); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink replaced: %v %v", st, err)
	}
	if b, _ := os.ReadFile(filepath.Join(v, "real/target.md")); !strings.Contains(string(b), "notes/x") {
		t.Errorf("target not rewritten: %q", b)
	}
	if st, _ := os.Stat(filepath.Join(v, "a/private.md")); st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", st.Mode().Perm())
	}
}

func TestMovePartialResultAfterWriteError(t *testing.T) {
	v := t.TempDir()
	write(t, v, ".obsidian/app.json", "{}")
	write(t, v, "inbox/x.md", "# x\n")
	write(t, v, "a/ok.md", "[[inbox/x]]\n")
	write(t, v, "b/ro/stuck.md", "[[inbox/x]]\n")
	failWriteOn(t, v, "b/ro/stuck.md")
	res, err := Move(v, "inbox/x.md", "notes/x.md")
	if err == nil {
		t.Fatal("expected a write error")
	}
	if want := []Moved{{"inbox/x.md", "notes/x.md"}}; !reflect.DeepEqual(res.Moves, want) {
		t.Errorf("Moves = %v, want %v", res.Moves, want)
	}
	if want := []string{"a/ok.md"}; !reflect.DeepEqual(res.Changed, want) {
		t.Errorf("Changed = %v, want %v", res.Changed, want)
	}
}

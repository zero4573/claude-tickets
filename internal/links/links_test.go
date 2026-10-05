package links

import (
	"os"
	"path/filepath"
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
	rel, n, err := Move(v, "projects/x/sequences/flow.md", "projects/x/features")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "projects/x/features/flow.md" || n != 1 {
		t.Errorf("Move = %s, %d", rel, n)
	}
	data, _ := os.ReadFile(filepath.Join(v, "projects/x/x.md"))
	if s := string(data); !strings.Contains(s, "[[flow]] and [[projects/x/features/flow#Steps|the flow]]") || !strings.Contains(s, "`[[not-a-link]]`") {
		t.Errorf("rewritten note:\n%s", s)
	}
	if _, _, err := Move(v, "inbox/a.md", "knowledge-base/x.md"); err == nil || !strings.Contains(err.Error(), "already used by projects/x/x.md") {
		t.Errorf("clash: %v", err)
	}
	if _, _, err := Move(v, "inbox/a.md", "references/dup.md"); err == nil || !strings.Contains(err.Error(), "destination exists") {
		t.Errorf("exists: %v", err)
	}
	if _, _, err := Move(v, "inbox/a.md", "/tmp/a.md"); err == nil || !strings.Contains(err.Error(), "inside the vault") {
		t.Errorf("outside: %v", err)
	}
}

func TestMoveRename(t *testing.T) {
	v := vault(t)
	write(t, v, "inbox/b.md", "[[x]] and [[X#Top|the x]] and `[[x]]`\n")
	rel, n, err := Move(v, "projects/x/x.md", "projects/x/x-overview.md")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "projects/x/x-overview.md" || n != 2 {
		t.Errorf("Move = %s, %d", rel, n)
	}
	if data, _ := os.ReadFile(filepath.Join(v, "inbox/b.md")); string(data) != "[[x-overview]] and [[x-overview#Top|the x]] and `[[x]]`\n" {
		t.Errorf("bare links not renamed:\n%s", data)
	}
	// dup.md exists twice: [[dup]] named the other one too, so it stays
	if _, _, err := Move(v, "references/dup.md", "references/dup-ref.md"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(v, "inbox/a.md")); string(data) != "[[dup]]\n" {
		t.Errorf("an ambiguous bare link was rewritten: %s", data)
	}
}

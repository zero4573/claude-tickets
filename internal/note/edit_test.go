package note

import (
	"os"
	"path/filepath"
	"testing"
)

func roundtrip(t *testing.T, in string, edit func(f string) error) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "n.md")
	if err := os.WriteFile(f, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := edit(f); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(f)
	return string(out)
}

func TestSetReplacesBlockLists(t *testing.T) {
	in := "---\nblocked-by:\n  - \"[[PROJ-2]]\"\n  - \"[[PROJ-3]]\"\nstatus: new\n---\n# X\n- not frontmatter\n"
	got := roundtrip(t, in, func(f string) error { return Set(f, "blocked-by", `["[[PROJ-4]]"]`) })
	want := "---\nblocked-by: [\"[[PROJ-4]]\"]\nstatus: new\n---\n# X\n- not frontmatter\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestEditTagsBlockStyle(t *testing.T) {
	in := "---\ntags:\n  - ticket\n  - \"jira\"\nstatus: new\n---\n"
	got := roundtrip(t, in, func(f string) error { return EditTags(f, true, "unassigned") })
	if want := "---\ntags: [ticket, jira, unassigned]\nstatus: new\n---\n"; got != want {
		t.Errorf("add: got %q\nwant %q", got, want)
	}
	got = roundtrip(t, in, func(f string) error { return EditTags(f, false, "jira") })
	if want := "---\ntags: [ticket]\nstatus: new\n---\n"; got != want {
		t.Errorf("remove: got %q\nwant %q", got, want)
	}
}

func TestCRLFAndBOM(t *testing.T) {
	in := "\ufeff---\r\nsource: jira\r\ntags:\r\n  - jira\r\nsource-id: PROJ-1\r\n---\r\n# PROJ-1\r\n"
	f := filepath.Join(t.TempDir(), "n.md")
	os.WriteFile(f, []byte(in), 0o644)
	fm := Fields(f)
	if fm["source"] != "jira" || fm["source-id"] != "PROJ-1" || fm["tags"] != "[jira]" {
		t.Errorf("Fields = %v", fm)
	}
	if Get(f, "source") != "jira" {
		t.Errorf("Frontmatter didn't read the CRLF note")
	}
	got := roundtrip(t, in, func(f string) error { return Set(f, "status", "closed") })
	if want := "---\nsource: jira\ntags:\n  - jira\nsource-id: PROJ-1\nstatus: closed\n---\n# PROJ-1\n"; got != want {
		t.Errorf("Set: got %q\nwant %q", got, want)
	}
}

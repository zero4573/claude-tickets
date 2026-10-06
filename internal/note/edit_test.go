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

func TestListField(t *testing.T) {
	cases := []struct {
		name, fm string
		want     []string
	}{
		{"flow", "depends-on: [a, b]\n", []string{"a", "b"}},
		{"flow quoted", "depends-on: [\"[[a]]\", '[[b]]', \"[[c, d]]\"]\n", []string{"[[a]]", "[[b]]", "[[c, d]]"}},
		{"block", "depends-on:\n  - \"[[a]]\"\n  - '[[b]]'\n- c\nstatus: x\n", []string{"[[a]]", "[[b]]", "c"}},
		{"empty flow", "depends-on: []\n", nil},
		{"empty block", "depends-on:\nstatus: x\n", nil},
		{"missing", "status: x\n", nil},
		{"scalar", "depends-on: a\n", []string{"a"}},
		{"quoted scalar", "depends-on: \"[[a]]\"\n", []string{"[[a]]"}},
		{"bare link", "depends-on: [[a]]\n", []string{"[[a]]"}},
		{"bare links", "depends-on: [[a|A]], [[b#h]]\n", []string{"[[a|A]]", "[[b#h]]"}},
		{"flow of bare links", "depends-on: [[[a]], [[b]]]\n", []string{"[[a]]", "[[b]]"}},
		{"empty scalar", "depends-on: \"\"\n", nil},
		{"null", "depends-on: null\n", nil},
		{"Null", "depends-on: Null\n", nil},
		{"NULL", "depends-on: NULL\n", nil},
		{"tilde", "depends-on: ~\n", nil},
		{"null with comment", "depends-on: null # none yet\n", nil},
		{"empty flow with comment", "depends-on: [] # none\n", nil},
		{"bare link with comment", "depends-on: [[a]] # x\n", []string{"[[a]]"}},
		{"heading kept", "depends-on: [[a#h]] # x\n", []string{"[[a#h]]"}},
		{"flow with comment", "depends-on: [\"[[a#h]]\", b] # two\n", []string{"[[a#h]]", "b"}},
		{"quoted hash kept", "depends-on: \"a #b\"\n", []string{"a #b"}},
		{"hash without space kept", "depends-on: a#b\n", []string{"a#b"}},
		{"comment only", "depends-on: # none\n", nil},
		{"block item with comment", "depends-on:\n  - \"[[a]]\" # x\n  - [[b#h]] # y\n", []string{"[[a]]", "[[b#h]]"}},
		{"other key prefix", "depends-on-x: [a]\n", nil},
	}
	for _, c := range cases {
		f := filepath.Join(t.TempDir(), "n.md")
		os.WriteFile(f, []byte("---\ntitle: n\n"+c.fm+"---\n- body: [z]\n"), 0o644)
		got := ListField(f, "depends-on")
		if len(got) != len(c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %q, want %q", c.name, got, c.want)
			}
		}
	}
	f := filepath.Join(t.TempDir(), "n.md")
	os.WriteFile(f, []byte("\ufeff---\r\ngroups:\r\n  - \"[[g]]\"\r\ndepends-on: [\"[[a]]\"]\r\n---\r\n"), 0o644)
	if g, d := ListField(f, "groups"), ListField(f, "depends-on"); len(g) != 1 || g[0] != "[[g]]" || len(d) != 1 || d[0] != "[[a]]" {
		t.Errorf("CRLF/BOM: groups %q, depends-on %q", g, d)
	}
	if got := ListField(filepath.Join(t.TempDir(), "missing.md"), "groups"); got != nil {
		t.Errorf("missing file: %q", got)
	}
}

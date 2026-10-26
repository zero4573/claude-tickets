package notesec

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zero4573/claude-tickets/assets"
	"github.com/zero4573/claude-tickets/internal/mdsec"
	"github.com/zero4573/claude-tickets/internal/vaultlock"
)

const note = "---\ntitle: T\ntags: [a, b]\nlist:\n  - x\n  - y\nstatus: open\n---\n# T\n\nintro\n\n## A\n\na body\n\n### A1\n\nsub\n\n## B\n\nb body\n"

// sameOutside checks the bytes before and after the changed span stayed.
func sameOutside(t *testing.T, before, after []byte, pre, suf string) {
	t.Helper()
	if !bytes.HasPrefix(after, []byte(pre)) || !bytes.HasSuffix(after, []byte(suf)) {
		t.Fatalf("bytes outside the span changed:\n%q\n-> %q", before, after)
	}
}

func TestOutlineGet(t *testing.T) {
	o := OutlineOf([]byte(note))
	if o.Frontmatter == nil || o.Frontmatter.StartLine != 1 || o.Frontmatter.EndLine != 8 {
		t.Fatalf("%+v", o.Frontmatter)
	}
	if len(o.Sections) != 4 || o.Sections[1].Title != "A" || o.Sections[1].StartLine != 13 || o.Sections[1].Tokens == 0 {
		t.Fatalf("%+v", o.Sections)
	}
	got, err := Get([]byte(note), "A", false)
	if err != nil || string(got) != "## A\n\na body\n\n### A1\n\nsub\n\n" {
		t.Fatalf("%q %v", got, err)
	}
	got, _ = Get([]byte(note), "A", true)
	if string(got) != "## A\n\na body\n\n" {
		t.Fatalf("%q", got)
	}
	if got, _ := Get([]byte(note), "", false); string(got) != note {
		t.Fatal("whole note")
	}
	if _, err := Get([]byte(note), "Z", false); !errors.Is(err, mdsec.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSetSection(t *testing.T) {
	src := []byte(note)
	out, tg, err := SetSection(src, "A", []byte("new\n\n\n"), false)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(note, "\na body\n\n### A1", "\nnew\n\n### A1", 1)
	if string(out) != want || strings.Join(tg.Section, ">") != "T>A" {
		t.Fatalf("%q", out)
	}
	sameOutside(t, src, out, "---\ntitle: T", "### A1\n\nsub\n\n## B\n\nb body\n")
	out, _, _ = SetSection(src, "A", []byte("all"), true)
	if string(out) != strings.Replace(note, "\na body\n\n### A1\n\nsub\n\n", "\nall\n\n", 1) {
		t.Fatalf("%q", out)
	}
	// last section, no blank line kept at EOF
	out, _, _ = SetSection(src, "B", []byte("x\n"), false)
	if !strings.HasSuffix(string(out), "## B\n\nx\n") {
		t.Fatalf("%q", out)
	}
	// unterminated heading at EOF
	out, _, _ = SetSection([]byte("# A\n## B"), "B", []byte("x"), false)
	if string(out) != "# A\n## B\nx\n" {
		t.Fatalf("%q", out)
	}
}

func TestSetSectionCRLF(t *testing.T) {
	src := []byte("# A\r\n\r\nx\r\n\r\n## B\r\n\r\ny\r\n")
	out, _, err := SetSection(src, "A", []byte("p\nq\n"), false)
	if err != nil || string(out) != "# A\r\n\r\np\r\nq\r\n\r\n## B\r\n\r\ny\r\n" {
		t.Fatalf("%q %v", out, err)
	}
	out, _, _ = Append(src, "B", []byte("z"), 0)
	if string(out) != "# A\r\n\r\nx\r\n\r\n## B\r\n\r\ny\r\nz\r\n" {
		t.Fatalf("%q", out)
	}
	out, _, _ = Append(src, "C", []byte("z"), 2)
	if string(out) != string(src)+"\r\n## C\r\n\r\nz\r\n" {
		t.Fatalf("%q", out)
	}
}

func TestAppend(t *testing.T) {
	src := []byte(note)
	out, _, err := Append(src, "A", []byte("more"), 0)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(note, "a body\n\n### A1", "a body\nmore\n\n### A1", 1)
	if string(out) != want {
		t.Fatalf("%q", out)
	}
	sameOutside(t, src, out, "---\ntitle: T", "\n### A1\n\nsub\n\n## B\n\nb body\n")
	out, _, _ = Append(src, "B", []byte("more\n"), 0)
	if string(out) != note+"more\n" {
		t.Fatalf("%q", out)
	}
	if _, _, err := Append(src, "Z", []byte("x"), 0); !errors.Is(err, mdsec.ErrNotFound) {
		t.Fatal(err)
	}
	out, tg, err := Append(src, "## Z", []byte("x"), 2)
	if err != nil || string(out) != note+"\n## Z\n\nx\n" || tg.Section[0] != "Z" {
		t.Fatalf("%q %v", out, err)
	}
	// empty section
	out, _, _ = Append([]byte("# A\n\n## B\n"), "A", []byte("x"), 0)
	if string(out) != "# A\n\nx\n\n## B\n" {
		t.Fatalf("%q", out)
	}
	// no trailing newline
	out, _, _ = Append([]byte("# A\ntext"), "A", []byte("x"), 0)
	if string(out) != "# A\ntext\nx\n" {
		t.Fatalf("%q", out)
	}
	if _, _, err := Append(src, "A > Z", []byte("x"), 2); err == nil {
		t.Fatal("create with a path")
	}
}

func TestFrontmatter(t *testing.T) {
	src := []byte(note)
	if v, ok := Field(src, "tags"); !ok || v != "[a, b]" {
		t.Fatal(v)
	}
	if v, _ := Field(src, "list"); v != "  - x\n  - y" {
		t.Fatalf("%q", v)
	}
	if len(Fields(src)) != 4 {
		t.Fatal(Fields(src))
	}
	j, err := FieldsJSON(src)
	if err != nil || j["status"] != "open" || len(j["list"].([]any)) != 2 {
		t.Fatalf("%v %v", j, err)
	}
	body := "# T" + strings.SplitN(note, "# T", 2)[1]
	chk := func(out []byte, err error, from, to string) {
		t.Helper()
		if err != nil || string(out) != strings.Replace(note, from, to, 1) {
			t.Fatalf("%q %v", out, err)
		}
		sameOutside(t, src, out, "---\ntitle: T", body)
	}
	out, err := SetField(src, "status", "done")
	chk(out, err, "status: open", "status: done")
	out, err = SetField(src, "status", "a: b")
	chk(out, err, "status: open", `status: "a: b"`)
	out, err = SetField(src, "ticket", "[[X]]")
	chk(out, err, "status: open\n---", "status: open\nticket: \"[[X]]\"\n---")
	out, err = SetField(src, "list", "one")
	chk(out, err, "list:\n  - x\n  - y", "list: one")
	out, err = AddItem(src, "tags", "c")
	chk(out, err, "[a, b]", "[a, b, c]")
	out, err = AddItem(src, "tags", "a")
	chk(out, err, "x", "x")
	out, err = RemoveItem(src, "tags", "a")
	chk(out, err, "[a, b]", "[b]")
	out, err = RemoveItem(src, "tags", "zz")
	chk(out, err, "x", "x")
	out, err = AddItem(src, "list", "z")
	chk(out, err, "  - y\n", "  - y\n  - z\n")
	out, err = RemoveItem(src, "list", "x")
	chk(out, err, "  - x\n", "")
	out, err = AddItem(src, "covers", "[[A]]")
	chk(out, err, "status: open\n---", "status: open\ncovers: [\"[[A]]\"]\n---")
	// no frontmatter at all
	out, err = SetField([]byte("# H\n"), "status", "x")
	if err != nil || string(out) != "---\nstatus: x\n---\n# H\n" {
		t.Fatalf("%q %v", out, err)
	}
	// CRLF
	out, err = SetField([]byte("---\r\na: 1\r\n---\r\nbody\r\n"), "a", "2")
	if err != nil || string(out) != "---\r\na: 2\r\n---\r\nbody\r\n" {
		t.Fatalf("%q %v", out, err)
	}
	out, err = AddItem([]byte("---\r\na: [x]\r\n---\r\nbody\r\n"), "b", "y")
	if err != nil || string(out) != "---\r\na: [x]\r\nb: [y]\r\n---\r\nbody\r\n" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := AddItem(src, "status", "x"); err == nil {
		t.Fatal("scalar is not a list")
	}
}

func vault(t *testing.T) string {
	v := t.TempDir()
	for _, d := range []string{".obsidian", "inbox", "projects/acme", "tickets/PROJ-1", "tickets/PROJ-2", "templates"} {
		os.MkdirAll(filepath.Join(v, d), 0o755)
	}
	w := func(rel, s string) { os.WriteFile(filepath.Join(v, rel), []byte(s), 0o644) }
	w("tickets/PROJ-1/PROJ-1.md", "---\ntitle: E\nsource: jira\nticket-type: epic\ncovers: [\"[[PROJ-2]]\"]\nstatus: open\n---\n# E\n\n## Description\n\nd\n\n## Tasks\n\nt\n")
	w("tickets/PROJ-2/PROJ-2.md", "---\ntitle: C\nsource: jira\nstatus: open\n---\n# C\n\n## Description\n\nd\n\n## Tasks\n\nt\n\n## Notes\n\nn\n")
	return v
}

func TestRules(t *testing.T) {
	v := vault(t)
	sec := func(s ...string) Target { return Target{Section: s} }
	fld := func(f string) Target { return Target{Field: f} }
	cases := []struct {
		name string
		c    Ctx
		rel  string
		tg   Target
		want string // "" allowed, else a substring of the refusal
	}{
		{"own folder", Ctx{v, "PROJ-1", false}, "tickets/PROJ-1/notes.md", sec("x"), ""},
		{"other folder", Ctx{v, "PROJ-1", false}, "projects/acme/a.md", sec("x"), "only inside tickets/PROJ-1/"},
		{"other ticket", Ctx{v, "PROJ-2", false}, "tickets/PROJ-1/PROJ-1.md", sec("Tasks"), "only inside tickets/PROJ-2/"},
		{"protected field", Ctx{v, "PROJ-2", false}, "tickets/PROJ-2/PROJ-2.md", fld("title"), "identity"},
		{"source field", Ctx{v, "PROJ-2", false}, "tickets/PROJ-2/PROJ-2.md", fld("source-url"), "identity"},
		{"status ok", Ctx{v, "PROJ-2", false}, "tickets/PROJ-2/PROJ-2.md", fld("status"), ""},
		{"protected section", Ctx{v, "PROJ-2", false}, "tickets/PROJ-2/PROJ-2.md", sec("C", "Description"), "Description"},
		{"work section", Ctx{v, "PROJ-2", false}, "tickets/PROJ-2/PROJ-2.md", sec("C", "Tasks"), ""},
		{"epic covers tasks", Ctx{v, "PROJ-1", false}, "tickets/PROJ-2/PROJ-2.md", sec("C", "Tasks"), ""},
		{"epic covers status", Ctx{v, "PROJ-1", false}, "tickets/PROJ-2/PROJ-2.md", fld("status"), ""},
		{"epic covers notes", Ctx{v, "PROJ-1", false}, "tickets/PROJ-2/PROJ-2.md", sec("C", "Notes"), "work sections"},
		{"epic covers tags", Ctx{v, "PROJ-1", false}, "tickets/PROJ-2/PROJ-2.md", fld("tags"), "work sections"},
		{"epic covers description", Ctx{v, "PROJ-1", false}, "tickets/PROJ-2/PROJ-2.md", sec("C", "Description"), "Description"},
		{"not epic", Ctx{v, "PROJ-2", false}, "tickets/PROJ-1/PROJ-1.md", sec("E", "Tasks"), "only inside"},
		{"inbox", Ctx{v, "", false}, "inbox/a.md", sec("x"), ""},
		{"no workspace", Ctx{v, "", false}, "projects/acme/a.md", sec("x"), "only inside inbox/"},
		{"no workspace tickets", Ctx{v, "", false}, "tickets/PROJ-2/PROJ-2.md", sec("x"), "only inside inbox/"},
		{"save projects", Ctx{v, "", true}, "projects/acme/a.md", sec("x"), ""},
		{"save in workspace", Ctx{v, "PROJ-1", true}, "projects/acme/a.md", sec("x"), ""},
		{"save raw", Ctx{v, "", true}, "raw/a.md", sec("x"), "raw/"},
		{"save templates", Ctx{v, "", true}, "templates/a.md", sec("x"), "templates/"},
		{"save archive", Ctx{v, "", true}, "archive/a.md", sec("x"), "archive/"},
		{"save dot dir", Ctx{v, "", true}, ".obsidian/a.md", sec("x"), "dot-"},
		{"save ticket field", Ctx{v, "", true}, "tickets/PROJ-2/PROJ-2.md", fld("covers"), "identity"},
		{"save ticket section", Ctx{v, "", true}, "tickets/PROJ-2/PROJ-2.md", sec("C", "Acceptance criteria"), "Acceptance"},
		{"save ticket status", Ctx{v, "", true}, "tickets/PROJ-2/PROJ-2.md", fld("status"), ""},
		{"escape", Ctx{v, "", true}, "../x.md", sec("x"), "outside the vault"},
	}
	for _, c := range cases {
		err := Check(c.c, c.rel, c.tg)
		var re *RefusedError
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: refused: %v", c.name, err)
		case c.want != "" && (!errors.As(err, &re) || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: want a refusal with %q, got %v", c.name, c.want, err)
		}
	}
}

func TestWorkspaceID(t *testing.T) {
	d := t.TempDir()
	sub := filepath.Join(d, "a", "b")
	os.MkdirAll(sub, 0o755)
	if WorkspaceID(sub) != "" {
		t.Skip("a workspace.json above the temp dir")
	}
	os.WriteFile(filepath.Join(d, "a", "workspace.json"), []byte(`{"repos":[]}`), 0o644)
	os.WriteFile(filepath.Join(d, "workspace.json"), []byte(`{"id":"PROJ-9"}`), 0o644)
	if got := WorkspaceID(sub); got != "PROJ-9" {
		t.Fatal(got)
	}
}

func TestApply(t *testing.T) {
	v := vault(t)
	rel := "inbox/n.md"
	os.WriteFile(filepath.Join(v, rel), []byte(note), 0o640)
	c := Ctx{Vault: v}
	op := func(src []byte) ([]byte, Target, error) {
		out, tg, err := SetSection(src, "B", []byte("new"), false)
		return out, tg, err
	}
	ch, err := Apply(c, rel, "me", op)
	if err != nil || !ch {
		t.Fatal(ch, err)
	}
	got, _ := os.ReadFile(filepath.Join(v, rel))
	if !strings.HasSuffix(string(got), "## B\n\nnew\n") {
		t.Fatalf("%q", got)
	}
	if st, _ := os.Stat(filepath.Join(v, rel)); st.Mode().Perm() != 0o640 {
		t.Fatal(st.Mode())
	}
	if s, _ := vaultlock.Status(v); s != "unlocked" {
		t.Fatal("lock not released:", s)
	}
	if ch, err := Apply(c, rel, "me", op); err != nil || ch {
		t.Fatal("unchanged write", ch, err)
	}
	// a refused write leaves the file and takes no lock
	if _, err := Apply(c, "projects/acme/x.md", "me", op); err == nil {
		t.Fatal("want an error")
	}
	// same owner holds the lock: used, not released
	if err := vaultlock.Acquire(v, "me"); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(c, rel, "me", func(s []byte) ([]byte, Target, error) { return append(s, 'x'), Target{}, nil }); err != nil {
		t.Fatal(err)
	}
	if s, _ := vaultlock.Status(v); s != "locked by 'me'" {
		t.Fatal(s)
	}
	// another owner: fails after the wait, file untouched
	old := vaultlock.Wait
	vaultlock.Wait = 100 * time.Millisecond
	defer func() { vaultlock.Wait = old }()
	before, _ := os.ReadFile(filepath.Join(v, rel))
	if _, err := Apply(c, rel, "other", func(s []byte) ([]byte, Target, error) { return append(s, 'y'), Target{}, nil }); err == nil || !strings.Contains(err.Error(), "still locked by 'me'") {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(v, rel))
	if !bytes.Equal(before, after) {
		t.Fatal("file changed")
	}
	vaultlock.Release(v, "me")
	if ents, _ := os.ReadDir(filepath.Join(v, "inbox")); len(ents) != 1 {
		t.Fatal("temp file left", ents)
	}
}

// W7-claude-tickets-C01: the guard messages cite AGENTS.md "Who writes what"
// item 3; the shipped scaffold keeps that heading and item.
func TestWhoWritesWhatPinned(t *testing.T) {
	data, err := fs.ReadFile(assets.Scaffold, "vault-scaffold/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	_, rest, ok := strings.Cut(s, "\n### Who writes what\n")
	if !ok {
		t.Fatal("vault-scaffold/AGENTS.md has no `### Who writes what` heading")
	}
	if !strings.Contains(rest, "\n3. A **ticket's agents**") {
		t.Error("`### Who writes what` has no numbered item 3 (A **ticket's agents** ...)")
	}
	if !strings.Contains(whoWrites, `"Who writes what" 3`) {
		t.Errorf("whoWrites no longer cites the item: %s", whoWrites)
	}
}

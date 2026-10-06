package links

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const url = "https://acme.atlassian.net/browse/PROJ-12"

func read(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// ticketVault has PROJ-12 (the ticket going), lookalikes, and a note with
// one link per line.
func ticketVault(t *testing.T, body string) string {
	v := t.TempDir()
	write(t, v, ".obsidian/app.json", "{}")
	write(t, v, "tickets/PROJ-12/PROJ-12.md", "---\nsource: jira\n---\n# PROJ-12\n[[PROJ-120]]\n")
	write(t, v, "tickets/PROJ-12/design.md", "[[PROJ-12]]\n")
	write(t, v, "tickets/PROJ-120/PROJ-120.md", "")
	write(t, v, "tickets/PROJ-1/PROJ-1.md", "")
	write(t, v, "templates/t.md", "[[PROJ-12]]\n")
	write(t, v, "inbox/n.md", body)
	return v
}

func rewrite(t *testing.T, v string, o RewriteOpts) (int, int) {
	t.Helper()
	n, notes, err := RewriteToURL(v, filepath.Join(v, "tickets/PROJ-12/PROJ-12.md"), "PROJ-12", url,
		[]string{filepath.Join(v, "tickets/PROJ-12")}, o)
	if err != nil {
		t.Fatal(err)
	}
	return n, notes
}

func TestRewriteToURLBody(t *testing.T) {
	md := "[PROJ-12](" + url + ")"
	for _, tc := range []struct{ in, want string }{
		{"[[PROJ-12]]", md},
		{"[[proj-12]]", md},
		{"[[PROJ-12.md]]", md},
		{"[[PROJ-12|the bug]]", "[the bug](" + url + ")"},
		{"[[PROJ-12#Review|review]]", "[review](" + url + ")"},
		{"[[PROJ-12#^abc]]", md},
		{"[[PROJ-12^abc]]", md},
		{"![[PROJ-12]]", md},
		{"![[PROJ-12#Review]]", md},
		{"![[PROJ-12|x]]", "[x](" + url + ")"},
		{"[[tickets/PROJ-12/PROJ-12]]", md},
		{"[[PROJ-12/PROJ-12]]", md},
		{"| [[PROJ-12\\|x]] |", "| [x](" + url + ") |"},
		{"`[[PROJ-12]]`", "`[[PROJ-12]]`"},
		{"[[PROJ-120]] [[PROJ-1]] [[#local]]", "[[PROJ-120]] [[PROJ-1]] [[#local]]"},
		{"<!-- [[PROJ-12]] -->", "<!-- " + md + " -->"},
		{"see [[PROJ-12]] and `[[PROJ-12]]` and [[PROJ-12]]", "see " + md + " and `[[PROJ-12]]` and " + md},
	} {
		v := ticketVault(t, tc.in+"\n")
		rewrite(t, v, RewriteOpts{})
		if got := read(t, v, "inbox/n.md"); got != tc.want+"\n" {
			t.Errorf("%s:\n got %q\nwant %q", tc.in, got, tc.want+"\n")
		}
	}
}

func TestRewriteToURLCode(t *testing.T) {
	in := "```\n[[PROJ-12]]\n```\n~~~md\n[[PROJ-12]]\n~~~\n[[PROJ-12]]\n```\n[[PROJ-12]] unclosed\n"
	v := ticketVault(t, in)
	n, notes := rewrite(t, v, RewriteOpts{})
	want := strings.Replace(in, "~~~\n[[PROJ-12]]", "~~~\n[PROJ-12]("+url+")", 1)
	if got := read(t, v, "inbox/n.md"); got != want || n != 1 || notes != 1 {
		t.Errorf("got %q (%d, %d)\nwant %q", got, n, notes, want)
	}
	if read(t, v, "templates/t.md") != "[[PROJ-12]]\n" || read(t, v, "tickets/PROJ-12/design.md") != "[[PROJ-12]]\n" {
		t.Error("templates/ or the ticket's folder rewritten")
	}
}

func TestRewriteToURLLineEndings(t *testing.T) {
	bom := "\xef\xbb\xbf"
	in := bom + "---\r\nrelated: [\"[[PROJ-12]]\"]\r\n---\r\n[[PROJ-12]]\r\n"
	v := ticketVault(t, in)
	rewrite(t, v, RewriteOpts{})
	want := bom + "---\r\nrelated: [\"[PROJ-12](" + url + ")\"]\r\n---\r\n[PROJ-12](" + url + ")\r\n"
	if got := read(t, v, "inbox/n.md"); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestRewriteToURLFrontmatter(t *testing.T) {
	md := "[PROJ-12](" + url + ")"
	in := "---\n" +
		"related: [\"[[PROJ-12]]\", \"[[PROJ-1]]\"]\n" +
		"covered-by: \"[[PROJ-12]]\"\n" +
		"parent: '[[PROJ-12]]'\n" +
		"blocks:\n  - \"[[PROJ-12]]\"\n  - [[PROJ-12]]\n" +
		"children: [[PROJ-12]]\n" +
		"summary: \"see [[PROJ-12]] too\"\n" +
		"---\nbody [[PROJ-12]]\n"
	want := "---\n" +
		"related: [\"" + md + "\", \"[[PROJ-1]]\"]\n" +
		"covered-by: \"" + md + "\"\n" +
		"parent: '" + md + "'\n" +
		"blocks:\n  - \"" + md + "\"\n  - \"" + md + "\"\n" +
		"children: \"" + md + "\"\n" +
		"summary: \"see [[PROJ-12]] too\"\n" +
		"---\nbody " + md + "\n"
	v := ticketVault(t, in)
	var not []string
	n, _ := rewrite(t, v, RewriteOpts{NotRewritten: func(p Place) { not = append(not, fmt.Sprintf("%s:%d", p.Field, p.Line)) }})
	if got := read(t, v, "inbox/n.md"); got != want || n != 7 {
		t.Errorf("got %q (%d)\nwant %q", got, n, want)
	}
	if strings.Join(not, ",") != "summary:9" {
		t.Errorf("not rewritten = %v", not)
	}
}

func TestRewriteToURLSkip(t *testing.T) {
	in := "---\nsource: jira\nrelated: [\"[[PROJ-12]]\"]\nprojects: [\"[[PROJ-12]]\"]\n---\n<!-- source:start -->\n[[PROJ-12]]\n<!-- source:end -->\n[[PROJ-12]]\n"
	v := ticketVault(t, in)
	var places []Place
	n, _ := rewrite(t, v, RewriteOpts{Skip: func(p Place) bool {
		places = append(places, p)
		return p.SourceBlock || p.Field == "related"
	}})
	md := "[PROJ-12](" + url + ")"
	want := "---\nsource: jira\nrelated: [\"[[PROJ-12]]\"]\nprojects: [\"" + md + "\"]\n---\n<!-- source:start -->\n[[PROJ-12]]\n<!-- source:end -->\n" + md + "\n"
	if got := read(t, v, "inbox/n.md"); got != want || n != 2 {
		t.Errorf("got %q (%d)\nwant %q", got, n, want)
	}
	if len(places) != 4 || !places[0].Frontmatter || places[2].Line != 7 || !places[2].SourceBlock || places[3].SourceBlock {
		t.Errorf("places = %+v", places)
	}
}

func TestRewriteToURLAmbiguousAndURL(t *testing.T) {
	v := ticketVault(t, "[[PROJ-12]] and [[tickets/PROJ-12/PROJ-12]]\n")
	write(t, v, "references/PROJ-12.md", "")
	rewrite(t, v, RewriteOpts{})
	// The bare link named the other PROJ-12.md too: left alone
	if got := read(t, v, "inbox/n.md"); got != "[[PROJ-12]] and [PROJ-12]("+url+")\n" {
		t.Errorf("ambiguous: %q", got)
	}

	v = ticketVault(t, "[[PROJ-12]]\n")
	if _, _, err := RewriteToURL(v, filepath.Join(v, "tickets/PROJ-12/PROJ-12.md"), "PROJ-12", "https://x/a(b)", nil, RewriteOpts{}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, v, "inbox/n.md"); got != "[PROJ-12](<https://x/a(b)>)\n" {
		t.Errorf("url with parentheses: %q", got)
	}
}

func TestRewriteToURLDryRun(t *testing.T) {
	v := ticketVault(t, "[[PROJ-12]] [[PROJ-12|x]]\n")
	write(t, v, "projects/p/p.md", "![[PROJ-12]]\n")
	var changed []string
	n, notes := rewrite(t, v, RewriteOpts{DryRun: true, Changed: func(f string) { changed = append(changed, f) }})
	if n != 3 || notes != 2 || len(changed) != 2 {
		t.Errorf("dry run counted %d link(s) in %d note(s), %v", n, notes, changed)
	}
	if read(t, v, "inbox/n.md") != "[[PROJ-12]] [[PROJ-12|x]]\n" {
		t.Error("dry run wrote")
	}
}

func TestReferenced(t *testing.T) {
	v := ticketVault(t, "[[PROJ-12]] [[tickets/PROJ-12/logs/2026-10-01-PROJ-12-fix]] ![[shot.png]] [[requirements]]\n")
	write(t, v, "tickets/PROJ-12/logs/2026-10-01-PROJ-12-fix.md", "![[trace.txt]] [[PROJ-12]]\n")
	write(t, v, "tickets/PROJ-12/trace.txt", "")
	write(t, v, "tickets/PROJ-12/shot.png", "")
	write(t, v, "tickets/PROJ-12/requirements.md", "")
	write(t, v, "tickets/PROJ-13/requirements.md", "")
	write(t, v, "tickets/PROJ-12/unused.png", "")
	write(t, v, "tickets/PROJ-12/qa-report.md", "")
	write(t, v, "tickets/PROJ-14/PROJ-14.md", "[[qa-report]]\n")
	got := Referenced(v, filepath.Join(v, "tickets/PROJ-12"), []string{filepath.Join(v, "tickets/PROJ-14")})
	var rels []string
	for _, p := range got {
		r, _ := filepath.Rel(resolvePath(v), p)
		rels = append(rels, filepath.ToSlash(r))
	}
	want := "tickets/PROJ-12/logs/2026-10-01-PROJ-12-fix.md tickets/PROJ-12/shot.png tickets/PROJ-12/trace.txt"
	if strings.Join(rels, " ") != want {
		t.Errorf("Referenced = %v\nwant %s", rels, want)
	}
	if got := Referenced(v, filepath.Join(v, "tickets/PROJ-12"), nil); len(got) != 4 {
		t.Errorf("without excluding PROJ-14: %v", got)
	}
}

func TestCheckWithExempt(t *testing.T) {
	v := t.TempDir()
	write(t, v, ".obsidian/app.json", "{}")
	write(t, v, "tickets/PROJ-2/PROJ-2.md", "---\nsource: jira\nrelated: [\"[[PROJ-9]]\"]\n---\n<!-- source:start -->\n[[PROJ-9]]\n<!-- source:end -->\n[[PROJ-9]]\n[[gone]]\n")
	exempt := func(p Place, target string) bool { return p.SourceBlock || p.Field == "related" }
	var out strings.Builder
	n := CheckWith(v, nil, &out, CheckOpts{Exempt: exempt, ExemptNote: "%d exempt link(s)"})
	got := out.String()
	if n != 2 || !strings.Contains(got, "2 exempt link(s)") || !strings.Contains(got, "PROJ-2.md:8: unresolved link [[PROJ-9]]") ||
		strings.Contains(got, ":3:") || strings.Contains(got, ":6:") {
		t.Errorf("CheckWith = %d:\n%s", n, got)
	}
	out.Reset()
	if n := Check(v, nil, &out); n != 4 {
		t.Errorf("Check = %d:\n%s", n, out.String())
	}
	out.Reset()
	write(t, v, "tickets/PROJ-2/PROJ-2.md", "---\nrelated: [\"[[PROJ-9]]\"]\n---\n| [[PROJ-2\\|me]] |\n")
	if n := CheckWith(v, nil, &out, CheckOpts{Exempt: exempt, ExemptNote: "%d exempt link(s)"}); n != 0 ||
		!strings.Contains(out.String(), "1 exempt link(s)\nct vault links: 1 file(s) checked, all links resolve") {
		t.Errorf("only exempt problems (and an escaped table pipe): %d\n%s", n, out.String())
	}
}

package ticketconv

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zero4573/tix/internal/note"
)

const stubTemplate = "---\ntitle: {{title}}\nsummary:\ncreated: {{date}}\nsource:\nsource-id: {{title}}\ncovers: []\ncovered-by:\ntags: [ticket]\n---\n# {{title}}\n\n<!-- source:start -->\n## Source\n<!-- source:end -->\n"

func stubVault(t *testing.T, lead string) (vault string) {
	t.Helper()
	vault = t.TempDir()
	write(t, filepath.Join(vault, ".obsidian", "app.json"), "{}")
	write(t, filepath.Join(vault, "tickets", ".sources.json"), `{"sources":{"jira":{"mcp":"atlassian"}}}`)
	write(t, filepath.Join(vault, "templates", "ticket-synced.md"), stubTemplate)
	write(t, filepath.Join(vault, "tickets", "manual", "MAN-1", "MAN-1.md"), lead)
	return vault
}

func stubOpts(v string) StubOptions {
	return StubOptions{Vault: v, ID: "PROJ-9", Source: "jira", Summary: "Add export", CoveredBy: "MAN-1", Today: "2026-10-29", Out: &bytes.Buffer{}}
}

func TestStubWritesNoteAndCovers(t *testing.T) {
	v := stubVault(t, "---\ntitle: MAN-1\ncovers: [\"[[PROJ-2]]\"]\ntags: [ticket]\n---\n# MAN-1\n")
	if err := Stub(stubOpts(v)); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(v, "tickets", "jira", "PROJ-9", "PROJ-9.md"))
	for _, want := range []string{"title: PROJ-9\n", "summary: Add export\n", "created: 2026-10-29\n", "source: jira\n",
		"source-id: PROJ-9\n", "covered-by: \"[[MAN-1]]\"\n", "tags: [ticket, jira, proj]\n", "<!-- source:start -->\n## Source\n<!-- source:end -->\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("stub lacks %q:\n%s", want, got)
		}
	}
	lead := filepath.Join(v, "tickets", "manual", "MAN-1", "MAN-1.md")
	if c := note.ListField(lead, "covers"); strings.Join(c, ",") != "[[PROJ-2]],[[PROJ-9]]" {
		t.Errorf("covers = %v", c)
	}
	// the lock is released
	if err := Stub(StubOptions{Vault: v, ID: "PROJ-10", Source: "jira", Summary: "x", CoveredBy: "MAN-1"}); err != nil {
		t.Errorf("second stub: %v", err)
	}
	if c := note.ListField(lead, "covers"); len(c) != 3 {
		t.Errorf("covers = %v", c)
	}
}

func TestStubSummaryQuoted(t *testing.T) {
	for in, want := range map[string]string{"Add export": "Add export", "Fix: totals": `"Fix: totals"`, `say "hi"`: `"say \"hi\""`, "- x": `"- x"`, "a\nb": "a b"} {
		if got := yamlScalar(in); got != want {
			t.Errorf("yamlScalar(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStubRefusals(t *testing.T) {
	lead := "---\ntitle: MAN-1\ncovers: []\n---\n# MAN-1\n"
	v := stubVault(t, lead)
	write(t, filepath.Join(v, "tickets", "jira", "PROJ-9", "PROJ-9.md"), "mine\n")
	for name, tc := range map[string]struct {
		mod  func(*StubOptions)
		want string
	}{
		"existing":  {func(o *StubOptions) {}, "already has a note"},
		"no lead":   {func(o *StubOptions) { o.ID, o.CoveredBy = "PROJ-8", "MAN-7" }, "no note for MAN-7"},
		"bad id":    {func(o *StubOptions) { o.ID = "nope" }, "not a ticket ID"},
		"manual":    {func(o *StubOptions) { o.ID, o.Source = "PROJ-8", "manual" }, "not a synced source"},
		"no source": {func(o *StubOptions) { o.ID, o.Source = "PROJ-8", "zen" }, "no source 'zen'"},
		"no text":   {func(o *StubOptions) { o.ID, o.Summary = "PROJ-8", " " }, "--summary"},
		"self":      {func(o *StubOptions) { o.ID = "MAN-1" }, "cover itself"},
	} {
		o := stubOpts(v)
		tc.mod(&o)
		err := Stub(o)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	if got := read(t, filepath.Join(v, "tickets", "jira", "PROJ-9", "PROJ-9.md")); got != "mine\n" {
		t.Errorf("existing note changed: %q", got)
	}
	if got := read(t, filepath.Join(v, "tickets", "manual", "MAN-1", "MAN-1.md")); got != lead {
		t.Errorf("lead changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(v, "tickets", "jira", "PROJ-8")); err == nil {
		t.Error("a refusal left a folder")
	}
}

func TestStubSyncedLeadKeepsSourceCovers(t *testing.T) {
	v := stubVault(t, "---\ntitle: MAN-1\n---\n# MAN-1\n")
	epic := "---\ntitle: PROJ-4\nsource: jira\nticket-type: epic\ncovers: [\"[[PROJ-2]]\"]\n---\n# PROJ-4\n"
	write(t, filepath.Join(v, "tickets", "jira", "PROJ-4", "PROJ-4.md"), epic)
	var out bytes.Buffer
	o := stubOpts(v)
	o.CoveredBy, o.Out = "PROJ-4", &out
	if err := Stub(o); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(v, "tickets", "jira", "PROJ-4", "PROJ-4.md")); got != epic {
		t.Errorf("a synced lead's note changed:\n%s", got)
	}
	if got := read(t, filepath.Join(v, "tickets", "jira", "PROJ-9", "PROJ-9.md")); !strings.Contains(got, "covered-by: \"[[PROJ-4]]\"\n") {
		t.Errorf("stub lacks covered-by:\n%s", got)
	}
	if !strings.Contains(out.String(), "left to the source") || strings.Contains(out.String(), "warning") {
		t.Errorf("output = %q", out.String())
	}
}

func TestStubLeadStatusAndType(t *testing.T) {
	for _, st := range []string{"closed", "done", "Done"} {
		lead := "---\ntitle: MAN-1\nstatus: " + st + "\ncovers: []\n---\n# MAN-1\n"
		v := stubVault(t, lead)
		err := Stub(stubOpts(v))
		if err == nil || !strings.Contains(err.Error(), "is "+strings.ToLower(st)) {
			t.Errorf("status %s: err = %v", st, err)
		}
		if _, err := os.Stat(filepath.Join(v, "tickets", "jira", "PROJ-9")); err == nil {
			t.Errorf("status %s: a refusal left a folder", st)
		}
	}
	v := stubVault(t, "---\ntitle: MAN-1\nstatus: new\nticket-type: story\ncovers: []\n---\n# MAN-1\n")
	var out bytes.Buffer
	o := stubOpts(v)
	o.Out = &out
	if err := Stub(o); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "warning: MAN-1 is not an epic") {
		t.Errorf("no warning: %q", out.String())
	}
}

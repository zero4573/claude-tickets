package note

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeFile(t *testing.T, v, rel, body string) {
	t.Helper()
	p := filepath.Join(v, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const testSources = `{"sources": {
  "manual": {"enabled": true, "sync": false},
  "jira": {"enabled": true, "mcp": "atlassian", "idPrefix": ""},
  "snow": {"enabled": true, "mcp": "snow", "idPrefix": "SNOW"},
  "zendesk": {"enabled": false, "mcp": "zendesk"}}}`

func TestValidSource(t *testing.T) {
	for s, want := range map[string]bool{"jira": true, "manual": true, "my_src-2": true, "": false, ".x": false,
		"-x": false, "a/b": false, `a\b`: false, "..": false, "PROJ-12": false, "MAN-1": false, "JIRA": true} {
		if got := ValidSource(s); got != want {
			t.Errorf("ValidSource(%q) = %v", s, got)
		}
	}
}

func TestGuessSource(t *testing.T) {
	v := t.TempDir()
	if got := GuessSource(v, "PROJ-12"); got != "manual" {
		t.Errorf("no .sources.json: %s", got)
	}
	writeFile(t, v, "tickets/.sources.json", testSources)
	for id, want := range map[string]string{"MAN-3": "manual", "SNOW-INC1": "snow", "PROJ-12": "jira"} {
		if got := GuessSource(v, id); got != want {
			t.Errorf("GuessSource(%s) = %s, want %s", id, got, want)
		}
	}
	writeFile(t, v, "tickets/.sources.json", `{"sources": {"manual": {}, "jira": {"enabled": false}, "zendesk": {"enabled": false, "idPrefix": "ZD"}}}`)
	if got := GuessSource(v, "PROJ-12"); got != "jira" {
		t.Errorf("a disabled keyed source: %s", got)
	}
	writeFile(t, v, "tickets/.sources.json", `{"sources": {"manual": {"idPrefix": "TODO"}}}`)
	if got := GuessSource(v, "TODO-1"); got != "manual" {
		t.Errorf("manual idPrefix: %s", got)
	}
	if got := GuessSource(v, "PROJ-1"); got != "manual" {
		t.Errorf("no keyed source: %s", got)
	}
}

func TestFindBothLayouts(t *testing.T) {
	v := t.TempDir()
	writeFile(t, v, "tickets/.sources.json", testSources)
	writeFile(t, v, "tickets/jira/PROJ-12/PROJ-12.md", "---\nsource: jira\nstatus: open\n---\n")
	writeFile(t, v, "tickets/jira/PROJ-12/plan.md", "plan")
	writeFile(t, v, "tickets/manual/MAN-1/MAN-1.md", "---\nsource: manual\nstatus: done\n---\n")
	writeFile(t, v, "tickets/PROJ-7/PROJ-7.md", "---\nsource: jira\nstatus: open\n---\n") // an old vault's
	writeFile(t, v, "tickets/manual/MAN-2/logs/x.md", "no note")

	if got := Find(v, "PROJ-12"); got != filepath.Join(v, "tickets", "jira", "PROJ-12", "PROJ-12.md") {
		t.Errorf("Find PROJ-12 = %s", got)
	}
	if got := Find(v, "PROJ-7"); got != filepath.Join(v, "tickets", "PROJ-7", "PROJ-7.md") {
		t.Errorf("Find old layout = %s", got)
	}
	if Find(v, "MAN-2") != "" || Find(v, "PROJ-99") != "" || Find(v, "../x") != "" {
		t.Error("Find of a ticket without a note")
	}
	if got := TicketPath(v, "PROJ-99"); got != filepath.Join(v, "tickets", "jira", "PROJ-99", "PROJ-99.md") {
		t.Errorf("TicketPath of a new ticket = %s", got)
	}
	if got := RelDir(v, "PROJ-12"); got != "tickets/jira/PROJ-12" {
		t.Errorf("RelDir = %s", got)
	}
	if got := RelDir(v, "PROJ-7"); got != "tickets/PROJ-7" {
		t.Errorf("RelDir old = %s", got)
	}
	var ids []string
	for _, l := range TicketNotes(v) {
		ids = append(ids, l.ID+":"+l.Source)
	}
	if want := []string{"MAN-1:manual", "PROJ-12:jira", "PROJ-7:"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("TicketNotes = %v", ids)
	}
	ids = nil
	for _, l := range TicketDirs(v) {
		ids = append(ids, l.ID)
	}
	if want := []string{"MAN-1", "MAN-2", "PROJ-12", "PROJ-7"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("TicketDirs = %v", ids)
	}
	var listed []string
	for _, tk := range List(v, true) {
		listed = append(listed, tk.ID)
	}
	if want := []string{"MAN-1", "PROJ-12", "PROJ-7"}; !reflect.DeepEqual(listed, want) {
		t.Errorf("List = %v", listed)
	}
	if got := SourceOf(v, "PROJ-7"); got != "jira" {
		t.Errorf("SourceOf old layout = %s", got)
	}
}

func TestArchiveDir(t *testing.T) {
	v := t.TempDir()
	writeFile(t, v, "tickets/.sources.json", testSources)
	writeFile(t, v, "tickets/manual/MAN-1/MAN-1.md", "---\nsource: manual\n---\n")
	writeFile(t, v, "archive/tickets/manual/MAN-2/MAN-2.md", "x")
	writeFile(t, v, "archive/tickets/MAN-3/MAN-3.md", "x")
	for id, want := range map[string]string{
		"MAN-1":   "archive/tickets/manual/MAN-1",
		"MAN-2":   "archive/tickets/manual/MAN-2",
		"MAN-3":   "archive/tickets/MAN-3",
		"PROJ-12": "archive/tickets/jira/PROJ-12",
	} {
		if got := ArchiveDir(v, id); got != filepath.Join(v, filepath.FromSlash(want)) {
			t.Errorf("ArchiveDir(%s) = %s", id, got)
		}
	}
	if got := ArchivedIDs(v); !reflect.DeepEqual(got, []string{"MAN-2", "MAN-3"}) {
		t.Errorf("ArchivedIDs = %v", got)
	}
}

func TestRelTicket(t *testing.T) {
	for _, tc := range []struct {
		rel, id, dir, rest string
		ok                 bool
	}{
		{"tickets/jira/PROJ-12/PROJ-12.md", "PROJ-12", "tickets/jira/PROJ-12", "PROJ-12.md", true},
		{"tickets/jira/PROJ-12/reviews/r.md", "PROJ-12", "tickets/jira/PROJ-12", "reviews/r.md", true},
		{"tickets/jira/PROJ-12", "PROJ-12", "tickets/jira/PROJ-12", "", true},
		{"tickets/PROJ-12/plan.md", "PROJ-12", "tickets/PROJ-12", "plan.md", true},
		{"tickets/.sources.json", "", "", "", false},
		{"tickets/jira/notes.md", "", "", "", false},
		{"projects/jira/PROJ-12/PROJ-12.md", "", "", "", false},
	} {
		id, dir, rest, ok := RelTicket(tc.rel)
		if id != tc.id || dir != tc.dir || rest != tc.rest || ok != tc.ok {
			t.Errorf("RelTicket(%s) = %s %s %s %v", tc.rel, id, dir, rest, ok)
		}
	}
	if id, ok := RelNote("tickets/manual/MAN-1/MAN-1.md"); !ok || id != "MAN-1" {
		t.Error("RelNote")
	}
	if _, ok := RelNote("tickets/manual/MAN-1/plan.md"); ok {
		t.Error("RelNote of a plan")
	}
}

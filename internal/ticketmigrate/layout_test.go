package ticketmigrate

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zero4573/claude-tickets/internal/links"
)

func layoutVault(t *testing.T) string {
	v := t.TempDir()
	put(t, v, "tickets/.sources.json", `{"sources": {"manual": {"enabled": true, "sync": false, "idPrefix": "MAN"},
  "jira": {"enabled": true, "mcp": "atlassian", "idPrefix": ""}, "snow": {"enabled": true, "idPrefix": "SNOW"}}}`)
	put(t, v, "tickets/.sync-state.json", "{}")
	put(t, v, "tickets/PROJ-12/PROJ-12.md", "---\nsource: jira\nstatus: open\n---\n# PROJ-12\n")
	put(t, v, "tickets/PROJ-12/plan.md", "plan\n")
	put(t, v, "tickets/PROJ-12/logs/2026-10-01.md", "log\n")
	put(t, v, "tickets/MAN-1/MAN-1.md", "---\nsource: manual\nstatus: new\n---\nsee [[PROJ-12]] and [[tickets/PROJ-12/plan|the plan]]\n")
	put(t, v, "tickets/SNOW-INC1/SNOW-INC1.md", "---\nstatus: new\n---\nno source field\n")
	put(t, v, "tickets/MAN-2/logs/x.md", "a reserved folder without its note\n")
	put(t, v, "tickets/jira/PROJ-3/PROJ-3.md", "---\nsource: jira\n---\nalready moved\n")
	put(t, v, "archive/tickets/MAN-4/MAN-4.md", "---\nsource: manual\n---\n")
	put(t, v, "archive/tickets/PROJ-9/design.md", "kept by ct clean\n")
	put(t, v, "projects/p/p.md", "![[tickets/PROJ-12/logs/2026-10-01]] [[archive/tickets/PROJ-9/design]] [[MAN-1]]\n")
	return v
}

func TestMigrateLayout(t *testing.T) {
	v := layoutVault(t)
	want := []Move{
		{"MAN-1", "manual", "tickets/MAN-1", "tickets/manual/MAN-1"},
		{"MAN-2", "manual", "tickets/MAN-2", "tickets/manual/MAN-2"},
		{"PROJ-12", "jira", "tickets/PROJ-12", "tickets/jira/PROJ-12"},
		{"SNOW-INC1", "snow", "tickets/SNOW-INC1", "tickets/snow/SNOW-INC1"},
		{"MAN-4", "manual", "archive/tickets/MAN-4", "archive/tickets/manual/MAN-4"},
		{"PROJ-9", "jira", "archive/tickets/PROJ-9", "archive/tickets/jira/PROJ-9"},
	}
	dry, err := MigrateLayout(v, true)
	if err != nil || !reflect.DeepEqual(dry.Moves, want) || len(dry.Files) != 0 {
		t.Fatalf("dry run: %+v %v", dry, err)
	}
	if _, err := os.Stat(filepath.Join(v, "tickets", "PROJ-12", "PROJ-12.md")); err != nil {
		t.Fatal("a dry run moved")
	}

	res, err := MigrateLayout(v, false)
	if err != nil || !reflect.DeepEqual(res.Moves, want) {
		t.Fatalf("%+v %v", res, err)
	}
	for _, rel := range []string{"tickets/jira/PROJ-12/PROJ-12.md", "tickets/jira/PROJ-12/plan.md", "tickets/jira/PROJ-12/logs/2026-10-01.md",
		"tickets/manual/MAN-1/MAN-1.md", "tickets/manual/MAN-2/logs/x.md", "tickets/snow/SNOW-INC1/SNOW-INC1.md", "tickets/jira/PROJ-3/PROJ-3.md",
		"archive/tickets/manual/MAN-4/MAN-4.md", "archive/tickets/jira/PROJ-9/design.md", "tickets/.sources.json", "tickets/.sync-state.json"} {
		if _, err := os.Stat(filepath.Join(v, filepath.FromSlash(rel))); err != nil {
			t.Errorf("missing %s", rel)
		}
	}
	for _, rel := range []string{"tickets/PROJ-12", "tickets/MAN-1", "tickets/MAN-2", "archive/tickets/MAN-4", "archive/tickets/PROJ-9"} {
		if _, err := os.Stat(filepath.Join(v, filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s left behind", rel)
		}
	}
	// path-qualified links follow; bare ones stay
	if got := get(t, v, "projects/p/p.md"); got != "![[tickets/jira/PROJ-12/logs/2026-10-01]] [[archive/tickets/jira/PROJ-9/design]] [[MAN-1]]\n" {
		t.Errorf("p.md: %q", got)
	}
	if got := get(t, v, "tickets/manual/MAN-1/MAN-1.md"); !strings.Contains(got, "[[PROJ-12]] and [[tickets/jira/PROJ-12/plan|the plan]]") {
		t.Errorf("MAN-1.md: %q", got)
	}
	if res.Rewritten != 3 {
		t.Errorf("rewritten %d", res.Rewritten)
	}
	rels := strings.Join(res.Rels(), " ")
	for _, r := range []string{"tickets/PROJ-12/PROJ-12.md", "tickets/jira/PROJ-12/PROJ-12.md", "projects/p/p.md"} {
		if !strings.Contains(rels, r) {
			t.Errorf("Rels lacks %s: %s", r, rels)
		}
	}
	// the note moves last
	if last := res.Files[len(res.Files)-1]; !strings.HasSuffix(last.To, ".md") || !strings.Contains(last.To, filepath.Base(filepath.Dir(last.To))+".md") {
		t.Errorf("last moved: %v", last)
	}

	// nothing left to do
	again, err := MigrateLayout(v, false)
	if err != nil || len(again.Moves) != 0 || len(again.Warnings) != 0 {
		t.Errorf("second run: %+v %v", again, err)
	}
}

func TestMigrateLayoutTakenAndOddSource(t *testing.T) {
	v := layoutVault(t)
	put(t, v, "tickets/jira/PROJ-12/plan.md", "a different plan\n")
	put(t, v, "tickets/MAN-1/MAN-1.md", "---\nsource: ../x\n---\n")
	res, err := MigrateLayout(v, false)
	if err != nil {
		t.Fatal(err)
	}
	w := strings.Join(res.Warnings, "\n")
	if !strings.Contains(w, "tickets/PROJ-12: not moved: tickets/jira/PROJ-12/plan.md exists already") ||
		!strings.Contains(w, `source "../x" can't name a folder`) {
		t.Errorf("warnings: %s", w)
	}
	if _, err := os.Stat(filepath.Join(v, "tickets", "PROJ-12", "PROJ-12.md")); err != nil {
		t.Error("the taken folder moved")
	}
	if _, err := os.Stat(filepath.Join(v, "tickets", "manual", "MAN-1", "MAN-1.md")); err != nil {
		t.Error("MAN-1 not placed by its ID")
	}
	for _, m := range res.Moves {
		if m.ID == "PROJ-12" {
			t.Errorf("reported as moved: %+v", m)
		}
	}
}

func TestMigrateLayoutStopsAndResumes(t *testing.T) {
	v := layoutVault(t)
	orig := moveTrees
	t.Cleanup(func() { moveTrees = orig })
	moveTrees = func(vault string, trees []links.Tree) (links.MoveResult, error) {
		// move one file of PROJ-12, then fail
		for _, tr := range trees {
			if filepath.Base(tr.Src) == "PROJ-12" {
				r, err := links.MoveTree(vault, tr.Src, tr.Dst, []string{filepath.Join(tr.Src, "plan.md")}, "")
				if err != nil {
					return r, err
				}
				return r, errors.New("disk full")
			}
		}
		t.Fatal("no PROJ-12")
		return links.MoveResult{}, nil
	}
	if _, err := MigrateLayout(v, false); err == nil {
		t.Fatal("no error")
	}
	moveTrees = orig
	if _, err := MigrateLayout(v, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(v, "tickets", "PROJ-12", "plan.md")); err == nil {
		t.Fatal("the stub didn't move plan.md")
	}
	for _, rel := range []string{"tickets/jira/PROJ-12/plan.md", "tickets/jira/PROJ-12/PROJ-12.md", "tickets/manual/MAN-2/logs/x.md"} {
		if _, err := os.Stat(filepath.Join(v, filepath.FromSlash(rel))); err != nil {
			t.Errorf("not resumed: %s", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(v, "tickets", "PROJ-12")); err == nil {
		t.Error("old folder left")
	}
	if got := get(t, v, "tickets/manual/MAN-1/MAN-1.md"); !strings.Contains(got, "[[tickets/jira/PROJ-12/plan|the plan]]") {
		t.Errorf("link after resume: %q", got)
	}
}

package ticketconv

import (
	"strings"
	"testing"
)

func TestRewriteID(t *testing.T) {
	in := strings.Join([]string{
		"[[MAN-4]] and [[MAN-4-status]] and [[MAN-4|alias]]",
		"MAN-40 XMAN-4 MAN-4x foo-MAN-4 stay",
		"path /w/MAN-4/tasks/x and MAN-4-09-name.md",
		"branch: task/MAN-4-09-name",
		"see task/MAN-4-09-name and MAN-4.",
		"converted-from: MAN-4",
		"title: MAN-4",
	}, "\n")
	want := strings.Join([]string{
		"[[PROJ-5]] and [[PROJ-5-status]] and [[PROJ-5|alias]]",
		"MAN-40 XMAN-4 MAN-4x foo-MAN-4 stay",
		"path /w/PROJ-5/tasks/x and PROJ-5-09-name.md",
		"branch: task/MAN-4-09-name",
		"see task/MAN-4-09-name and PROJ-5.",
		"converted-from: MAN-4",
		"title: PROJ-5",
	}, "\n")
	if got := RewriteID(in, "MAN-4", "PROJ-5"); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := RewriteID(in, "MAN-5", "X-1"); got != in {
		t.Errorf("no match changed the text")
	}
}

func TestRenameFile(t *testing.T) {
	r := RenameFile("MAN-4", "PROJ-5")
	for in, want := range map[string]string{
		"MAN-4.md": "PROJ-5.md", "MAN-4-status.md": "PROJ-5-status.md", "MAN-4-09-x.md": "PROJ-5-09-x.md",
		"MAN-40.md": "MAN-40.md", "board.md": "board.md", "MAN-4": "MAN-4",
	} {
		if got := r(in); got != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
}

const manual = `---
title: MAN-4
summary: "A thing"
ticket-type: bug
source: manual
source-id: MAN-4
convert-source: jira
convert-key: PROJ-5
convert-url: https://x/browse/PROJ-5
tags: [ticket, manual]
---
# MAN-4: A thing

<!-- note -->

## Description
It breaks.

## Acceptance criteria
- [ ] fixed
- [x] tested
1. [] third

## Context / links
Nope.
`

func TestFlip(t *testing.T) {
	got := Flip(manual, "jira", "PROJ-5", "https://x/browse/PROJ-5", "MAN-4", "Jira")
	for _, want := range []string{
		"source: jira\n", "source-id: PROJ-5\n", "source-url: https://x/browse/PROJ-5\n", "converted-from: MAN-4\n",
		"tags: [ticket, jira, proj]\n", "summary: \"A thing\"\n",
		"# MAN-4: A thing\n\n<!-- source:start -->\n## Source (Jira)\n<!-- source:end -->\n\n<!-- note -->",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, bad := range []string{"convert-", "source: manual", "manual]"} {
		if strings.Contains(got, bad) {
			t.Errorf("still has %q", bad)
		}
	}
	if again := Flip(got, "jira", "PROJ-5", "https://x/browse/PROJ-5", "MAN-4", "Jira"); again != got {
		t.Errorf("not stable:\n%s", again)
	}
	if Flip("no frontmatter", "jira", "P-1", "u", "M-1", "Jira") != "no frontmatter" {
		t.Error("changed a note without frontmatter")
	}
}

func TestSetFields(t *testing.T) {
	got := SetFields("---\na: 1\nlist:\n  - x\nb: 2\n---\nbody\n", "list", "y", "c", "3", "a", "")
	if want := "---\nlist: y\nb: 2\nc: 3\n---\nbody\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if Field(got, "c") != "3" || Field("---\nk: \"q\"\n---\n", "k") != "q" {
		t.Error("Field")
	}
}

func TestDescription(t *testing.T) {
	got := Description(manual)
	want := "It breaks.\n\n### Acceptance criteria\n- \\[ \\] fixed\n- \\[x\\] tested\n1. \\[ \\] third"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if !HasTaskList(manual) || HasTaskList("plain [x] marks the spot") {
		t.Error("HasTaskList")
	}
	if Description("---\n---\n## Context / links\nx\n") != "" {
		t.Error("description without sections")
	}
}

func TestRewriteWorkspace(t *testing.T) {
	doc := map[string]any{"id": "MAN-4", "repos": []any{
		map[string]any{"slug": "a", "path": "/w/MAN-4/a"},
		map[string]any{"slug": "b", "path": "/elsewhere/MAN-4/b"},
	}}
	if !RewriteWorkspace(doc, "PROJ-5", "/w/MAN-4", "/w/PROJ-5") {
		t.Fatal("no change reported")
	}
	repos := doc["repos"].([]any)
	if doc["id"] != "PROJ-5" || repos[0].(map[string]any)["path"] != "/w/PROJ-5/a" || repos[1].(map[string]any)["path"] != "/elsewhere/MAN-4/b" {
		t.Errorf("got %v", doc)
	}
	if RewriteWorkspace(doc, "PROJ-5", "/w/MAN-4", "/w/PROJ-5") {
		t.Error("second run changed something")
	}
}

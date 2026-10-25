package ticketmigrate

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/zero4573/claude-tickets/assets"
)

const v1Board = `---
title: Board
status: active
---
# Board

Tasks of [[X]]; one row each, updated in place.

## Tasks
<!-- Status: planned · running · orphaned · merged <sha> · partial · blocked · dropped · pending — user. Note: ≤1 line. -->
| ID | Task | Repo | Wave | Deps | Status | Note |
|---|---|---|---|---|---|---|
| 01 | [[X-01-a]] | acme-api | 1 | - | merged abc123 | R-03 |
| 02 | [[X-02-b\|label]] | acme-ui | 1 | 01 | running | a · b |
| 03 | plain text | acme-ui | 2 | 01, 02 | planned | |
| 04 | [[X-04-d]] | acme-ui | 2 | - | dropped | no longer needed |
| 05 | [[X-05-e]] | acme-ui | 2 | - | pending — user | question |
| 06 | [[X-06-f]] | acme-ui | 2 | - | weird | |

## Reviews
<!-- M/S/C: counts. -->
| Wave | Repo | Range | M/S/C | File | Fix tasks |
|---|---|---|---|---|---|
| 1 | acme-api | a..b | 1/2/0 | [[wave-1-acme-api]] | 03 |
| 4 fix | acme-api | b..c | - | lead checked | - |
`

func lines(s string) []string { return strings.Split(strings.TrimRight(s, "\n"), "\n") }

func TestConvertBoard(t *testing.T) {
	c := ConvertBoard([]byte(v1Board))
	if len(c.Problems) != 0 || !c.Changed {
		t.Fatalf("%+v", c)
	}
	got := string(c.Data)
	for _, want := range []string{
		"- [x] 01 · merged abc123 · [[X-01-a]] · acme-api · w1 · deps - · R-03",
		"- [/] 02 · running · [[X-02-b|label]] · acme-ui · w1 · deps 01 · a - b",
		"- [ ] 03 · planned · plain text · acme-ui · w2 · deps 01, 02 · -",
		"- [-] 04 · dropped · [[X-04-d]] · acme-ui · w2 · deps - · no longer needed",
		"- [/] 05 · pending — user · [[X-05-e]] · acme-ui · w2 · deps - · question",
		"- [/] 06 · weird · [[X-06-f]] · acme-ui · w2 · deps - · -",
		"- w1 · acme-api · a..b · M/S/C 1/2/0 · [[wave-1-acme-api]] · fix tasks 03",
		"- w4 fix · acme-api · b..c · M/S/C - · lead checked · fix tasks -",
		"one row each, updated in place.",
		TasksComment + "\n- [x] 01",
		ReviewsComment + "\n- w1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "| ID |") || strings.Contains(got, "|---|") || strings.Contains(got, "Status: planned") {
		t.Errorf("table left:\n%s", got)
	}
	if len(c.Warnings) != 1 || c.Warnings[0].Line != 18 || !strings.Contains(c.Warnings[0].Why, "weird") {
		t.Errorf("warnings %+v", c.Warnings)
	}
	// idempotent
	again := ConvertBoard(c.Data)
	if again.Changed || string(again.Data) != got {
		t.Error("second conversion changed the board")
	}
}

func TestConvertBoardStatusMapping(t *testing.T) {
	for status, box := range map[string]string{
		"planned": " ", "running": "/", "orphaned": "/", "partial": "/", "blocked": "/",
		"pending — user": "/", "merged": "x", "merged 1cf4eb1": "x", "dropped": "-", "Planned": " ",
	} {
		in := "| ID | Task | Repo | Wave | Deps | Status | Note |\n|---|---|---|---|---|---|---|\n| 01 | t | r | 1 | - | " + status + " | n |\n"
		c := ConvertBoard([]byte(in))
		want := "- [" + box + "] 01 · " + status + " · t · r · w1 · deps - · n\n"
		if string(c.Data) != want || len(c.Warnings) != 0 {
			t.Errorf("%s: %q %+v", status, c.Data, c.Warnings)
		}
	}
}

func TestConvertBoardParseOrLeave(t *testing.T) {
	in := strings.Replace(v1Board, "| 03 | plain text | acme-ui | 2 | 01, 02 | planned | |", "| 03 | plain text | acme-ui | 2 | planned |", 1)
	c := ConvertBoard([]byte(in))
	if c.Changed || string(c.Data) != in || len(c.Problems) != 1 || c.Problems[0].Line != 15 {
		t.Fatalf("%+v", c.Problems)
	}
	nosep := "| ID | Task | Repo | Wave | Deps | Status | Note |\nfoo\n"
	if c := ConvertBoard([]byte(nosep)); c.Changed || len(c.Problems) != 1 {
		t.Errorf("%+v", c)
	}
	// not a v1 header: untouched, no problem
	other := "| A | B |\n|---|---|\n| 1 | 2 |\n"
	if c := ConvertBoard([]byte(other)); c.Changed || len(c.Problems) != 0 {
		t.Errorf("%+v", c)
	}
	// already v2
	v2 := TasksComment + "\n- [ ] 01 · planned · [[X-01-a]] · r · w1 · deps - · n\n"
	if c := ConvertBoard([]byte(v2)); c.Changed {
		t.Error("a v2 board changed")
	}
}

func TestConvertBoardCRLFBOMNoFinalNewline(t *testing.T) {
	crlf := "\xef\xbb\xbf" + strings.ReplaceAll(v1Board, "\n", "\r\n")
	c := ConvertBoard([]byte(crlf))
	if len(c.Problems) != 0 || !c.Changed {
		t.Fatalf("%+v", c)
	}
	s := string(c.Data)
	if !strings.HasPrefix(s, "\xef\xbb\xbf---\r\n") || strings.Count(s, "\r\n") != strings.Count(s, "\n") {
		t.Errorf("line endings or BOM lost: %q", s[:60])
	}
	if !strings.Contains(s, "- [x] 01 · merged abc123 · [[X-01-a]] · acme-api · w1 · deps - · R-03\r\n") {
		t.Error("task line not CRLF")
	}
	if !strings.HasSuffix(s, "\r\n") {
		t.Error("final newline lost")
	}
	// the same file in LF equals it with the endings swapped
	lf := ConvertBoard([]byte(v1Board))
	if strings.ReplaceAll(strings.TrimPrefix(s, "\xef\xbb\xbf"), "\r\n", "\n") != string(lf.Data) {
		t.Error("CRLF conversion differs from LF")
	}
	if c := ConvertBoard(c.Data); c.Changed {
		t.Error("not idempotent")
	}
	// no final newline
	nl := strings.TrimRight(v1Board, "\n")
	c = ConvertBoard([]byte(nl))
	if strings.HasSuffix(string(c.Data), "\n") {
		t.Error("a final newline was added")
	}
}

const v1Review = `---
type: review
---
# R

## Must-fix
- W1-acme-api-M01 · a.go:1 · p · f · t
- W1-acme-api-M02 · a.go:2 · p · f · t

## Dispositions
<!-- The lead's: one line per finding, ` + "`<ID> · resolved · <how>`, `<ID> · resolved · kept: <reason>` or `<ID> · deferred · [[<ticket>]] · <why>`" + ` -->
<!-- The reviewer lists every finding ID of Must-fix, Should-fix and Consider as ` + "`- <ID> · open`" + `. -->
- W1-acme-api-M01 · open
- W1-acme-api-M02 · resolved · task 12 (1cf4eb1)
- W1-acme-api-S01 · resolved · kept: fine as it is
- W1-acme-api-S02 · deferred · [[PROJ-40]] · later
- W1-acme-api-S03 · deferred · [[PROJ-41]]
- [x] W1-acme-api-C01 · resolved · done
free text stays

## Consider
- W1-acme-api-C02 · b.go:1 · p · f · t
`

func TestConvertReview(t *testing.T) {
	c := ConvertReview([]byte(v1Review))
	if len(c.Problems) != 0 || !c.Changed {
		t.Fatalf("%+v", c)
	}
	got := string(c.Data)
	for _, want := range []string{
		"- [ ] W1-acme-api-M01 · open\n",
		"- [x] W1-acme-api-M02 · resolved · task 12 (1cf4eb1)\n",
		"- [-] W1-acme-api-S01 · kept: fine as it is\n",
		"- [x] W1-acme-api-S02 · deferred → [[PROJ-40]] · later #deferred\n",
		"- [x] W1-acme-api-S03 · deferred → [[PROJ-41]] · - #deferred\n",
		"- [x] W1-acme-api-C01 · resolved · done\nfree text stays\n",
		"## Dispositions\n" + DispositionsComment + "\n- [ ] W1-acme-api-M01",
		"## Must-fix\n- W1-acme-api-M01 · a.go:1", // findings outside Dispositions stay
		"## Consider\n- W1-acme-api-C02 · b.go:1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "The lead's") {
		t.Error("v1 comment left")
	}
	for _, l := range lines(DispositionsComment) {
		if strings.HasPrefix(strings.TrimSpace(l), "- [") {
			t.Errorf("comment line looks like a checkbox: %q", l)
		}
	}
	if c := ConvertReview(c.Data); c.Changed {
		t.Error("not idempotent")
	}
	// CRLF
	crlf := ConvertReview([]byte(strings.ReplaceAll(v1Review, "\n", "\r\n")))
	if strings.ReplaceAll(string(crlf.Data), "\r\n", "\n") != got || strings.Count(string(crlf.Data), "\r\n") != strings.Count(string(crlf.Data), "\n") {
		t.Error("CRLF round-trip")
	}
}

func TestConvertReviewNoKnownForm(t *testing.T) {
	in := strings.Replace(v1Review, "- W1-acme-api-M01 · open", "- W1-acme-api-M01 · fixed", 1)
	c := ConvertReview([]byte(in))
	if c.Changed || string(c.Data) != in || len(c.Problems) != 1 || c.Problems[0].Line != 13 || c.Problems[0].Why != "no known form" {
		t.Fatalf("%+v", c)
	}
	// "resolved" with nothing after, "deferred" with no ticket
	for _, l := range []string{"- W1-a-M01 · resolved", "- W1-a-M01 · deferred · later"} {
		in := "## Dispositions\n" + l + "\n"
		if c := ConvertReview([]byte(in)); c.Changed || len(c.Problems) != 1 {
			t.Errorf("%q: %+v", l, c)
		}
	}
	// no Dispositions section: untouched
	if c := ConvertReview([]byte("# R\n- W1-a-M01 · open\n")); c.Changed {
		t.Error("changed outside Dispositions")
	}
}

func TestConvertBacklog(t *testing.T) {
	in := "# Backlog\n\n## Open\n<!-- ≤40: `- [ ] B-nn · w<N>` -->\n- B-01 · w1 · api · retry · W1-api-S01\nB-02 · w1 · ui · bare\n- [ ] B-03 · w2 · done already\n\n## Closed\n- B-00 · w1 · old\n"
	c := ConvertBacklog([]byte(in))
	want := "# Backlog\n\n## Open\n<!-- ≤40: `- [ ] B-nn · w<N>` -->\n- [ ] B-01 · w1 · api · retry · W1-api-S01\n- [ ] B-02 · w1 · ui · bare\n- [ ] B-03 · w2 · done already\n\n## Closed\n- B-00 · w1 · old\n"
	if string(c.Data) != want || !c.Changed {
		t.Fatalf("%q", c.Data)
	}
	if c := ConvertBacklog(c.Data); c.Changed {
		t.Error("not idempotent")
	}
	crlf := ConvertBacklog([]byte(strings.ReplaceAll(in, "\n", "\r\n")))
	if strings.ReplaceAll(string(crlf.Data), "\r\n", "\n") != want {
		t.Error("CRLF")
	}
}

// W6-claude-tickets-M01: the comments equal the shipped templates' text byte
// for byte, so a template edit breaks the build until they follow.
func TestCommentsMatchShippedTemplates(t *testing.T) {
	read := func(name string) string {
		b, err := fs.ReadFile(assets.Scaffold, "vault-scaffold/templates/"+name+".md")
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(b), "\r\n", "\n")
	}
	board, review := read("board"), read("review")
	for name, c := range map[string]struct{ tpl, comment string }{
		"TasksComment":        {board, TasksComment},
		"ReviewsComment":      {board, ReviewsComment},
		"DispositionsComment": {review, DispositionsComment},
	} {
		if !strings.Contains(c.tpl, "\n"+c.comment+"\n") {
			t.Errorf("%s is not the shipped template's comment", name)
		}
	}
}

// W6-claude-tickets-C01: any v1 Dispositions comment is replaced.
func TestConvertReviewReplacesComment_v1Variants(t *testing.T) {
	in := "## Dispositions\n" +
		"<!-- The lead's: one line per finding, `<ID> · resolved · <how>`, or `<ID> · deferred · [[<ticket>]] · <why> ([[review-disposition-v1]])` -->\n" +
		"- W1-acme-api-M01 · open\n"
	c := ConvertReview([]byte(in))
	if !c.Changed || strings.Contains(string(c.Data), "The lead's") || !strings.Contains(string(c.Data), DispositionsComment+"\n- [ ] W1-acme-api-M01") {
		t.Errorf("not replaced:\n%s", c.Data)
	}
}

// W6-claude-tickets-C02: a v1 table quoted in a code fence or an HTML
// comment is left alone.
func TestConvertBoardSkipsFencesAndComments(t *testing.T) {
	table := "| ID | Task | Repo | Wave | Deps | Status | Note |\n|---|---|---|---|---|---|---|\n| 01 | [[X-01-a]] | r | 1 | - | planned | n |\n"
	for name, in := range map[string]string{
		"fence":   "# Board\n\n```\n" + table + "```\n",
		"comment": "# Board\n\n<!--\n" + table + "-->\n",
	} {
		c := ConvertBoard([]byte(in))
		if c.Changed || string(c.Data) != in || len(c.Problems) != 0 {
			t.Errorf("%s: changed:\n%s", name, c.Data)
		}
	}
	// a real table after a fence is still converted
	c := ConvertBoard([]byte("```\nx\n```\n" + table))
	if !c.Changed || !strings.Contains(string(c.Data), "- [ ] 01") {
		t.Errorf("table after a fence not converted:\n%s", c.Data)
	}
}

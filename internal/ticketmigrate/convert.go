// Package ticketmigrate converts a ticket's v1 files (board tables,
// bare review dispositions, bare backlog items) to the checklist formats of
// the vault-checklists contract, for tix vault update. The converters are
// pure (bytes in, bytes out) and never guess: a file they can't parse is
// left whole, with one problem per line. They keep the file's line
// endings, final newline and BOM, and converting twice equals once.
package ticketmigrate

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// The v2 comments the converters put where v1 comments were. The board's
// Tasks one is the text of the checklists contract; each is pinned by a
// test against the shipped templates.
const (
	TasksComment = "<!-- One line per task: `- [<s>] NN · <status> · [[<ID>-NN-<slug>]] · <repo> · w<N> · deps <NN, …|-> · <note ≤1 line> ➕ <date>`\n" +
		"     `[ ]` planned · `[/]` running, orphaned, partial, blocked, pending — user · `[x]` merged <sha> ✅ <date> · `[-]` dropped ❌ <date> -->"
	ReviewsComment = "<!-- Plain bullets (a review is not a task): `- w<N> · <repo> · <from>..<to> · M/S/C <m>/<s>/<c> · [[wave-N-<slug>]] · fix tasks <NN, …|lead <sha>|->`\n" +
		"     a pass with no review file: `- w<N> fix · <repo> · <from>..<to> · - · <what the lead checked> · -` -->"

	// the Dispositions comment of a v1 review, and what replaces it
	dispositionsV1a     = "<!-- The lead's: one line per finding," // prefix of every v1 comment
	dispositionsV1b     = "<!-- The reviewer lists every finding ID of Must-fix, Should-fix and Consider as `- <ID> · open`. -->"
	DispositionsComment = "<!-- The reviewer writes one line per finding ID of Must-fix, Should-fix and Consider: `- [ ] <ID> · open`. The lead keeps exactly one line per ID and sets box, word and date: `- [/] <ID> · fixing · task NN`, `- [x] <ID> · resolved · task NN (<sha>) ✅ <date>`, `- [-] <ID> · kept: <reason ≤150 chars> ❌ <date>`, `- [x] <ID> · deferred → [[<ticket>]] · <why> #deferred ✅ <date>` -->"
)

var (
	v1Tasks   = []string{"ID", "Task", "Repo", "Wave", "Deps", "Status", "Note"}
	v1Reviews = []string{"Wave", "Repo", "Range", "M/S/C", "File", "Fix tasks"}

	sepCell    = regexp.MustCompile(`^:?-+:?$`)
	findingID  = `W[0-9]+-[a-z0-9-]+-[MSC][0-9]+`
	dispLine   = regexp.MustCompile(`^- (` + findingID + `) · (.*)$`)
	deferredRe = regexp.MustCompile(`^deferred · \[\[([^\]]+)\]\](?: · (.*))?$`)
	backlogRe  = regexp.MustCompile(`^(?:- )?(B-[0-9]+) · (.*)$`)
)

// Problem is a line a converter can't convert.
type Problem struct {
	Line int // 1-based
	Why  string
}

// Converted is a converter's result. With Problems the file is left whole:
// Data is the input and Changed false. Warnings are lines converted on a
// best guess.
type Converted struct {
	Data     []byte
	Changed  bool
	Problems []Problem
	Warnings []Problem
}

type line struct {
	text string
	cr   bool // the line ended in \r\n (or \r at the very end)
}

type doc struct {
	bom   bool
	lines []line
}

func split(in []byte) doc {
	d := doc{}
	if bytes.HasPrefix(in, []byte("\xef\xbb\xbf")) {
		d.bom = true
		in = in[3:]
	}
	for _, p := range strings.Split(string(in), "\n") {
		l := line{text: p}
		if strings.HasSuffix(p, "\r") {
			l.text, l.cr = strings.TrimSuffix(p, "\r"), true
		}
		d.lines = append(d.lines, l)
	}
	return d
}

func (d doc) join(lines []line) []byte {
	var b strings.Builder
	if d.bom {
		b.WriteString("\xef\xbb\xbf")
	}
	for i, l := range lines {
		b.WriteString(l.text)
		if l.cr {
			b.WriteString("\r")
		}
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

func (d doc) result(out []line, in []byte, problems, warnings []Problem) Converted {
	if len(problems) > 0 {
		return Converted{Data: in, Problems: problems, Warnings: warnings}
	}
	data := d.join(out)
	return Converted{Data: data, Changed: !bytes.Equal(data, in), Warnings: warnings}
}

// tracker follows HTML comments: lines inside one (and its first and last
// lines) never convert.
type tracker struct{ open bool }

func (c *tracker) comment(text string) bool {
	t := strings.TrimSpace(text)
	if !c.open && !strings.HasPrefix(t, "<!--") {
		return false
	}
	c.open = !strings.Contains(t, "-->")
	return true
}

// splitRow splits a table row on unescaped pipes; \| becomes |. ok is false
// for a line that isn't a row.
func splitRow(text string) (cells []string, ok bool) {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "|") {
		return nil, false
	}
	t = t[1:]
	var cur strings.Builder
	for i := 0; i < len(t); i++ {
		switch {
		case t[i] == '\\' && i+1 < len(t) && t[i+1] == '|':
			cur.WriteByte('|')
			i++
		case t[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(t[i])
		}
	}
	if rest := strings.TrimSpace(cur.String()); rest != "" {
		cells = append(cells, rest)
	}
	return cells, true
}

func sameCells(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isSeparator(text string) bool {
	cells, ok := splitRow(text)
	if !ok || len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if !sepCell.MatchString(c) {
			return false
		}
	}
	return true
}

func orDash(s string) string {
	if s = strings.TrimSpace(s); s == "" {
		return "-"
	}
	return s
}

// noDot makes a cell safe inside a · separated line.
func noDot(s string) string { return orDash(strings.ReplaceAll(s, " · ", " - ")) }

func waveLabel(s string) string {
	s = orDash(s)
	if s[0] >= '0' && s[0] <= '9' {
		return "w" + s
	}
	return s
}

// boxFor is the checkbox state for a v1 task status.
func boxFor(status string) (box byte, known bool) {
	s := strings.ToLower(strings.TrimSpace(status))
	switch {
	case s == "planned":
		return ' ', true
	case s == "running", s == "orphaned", s == "partial", s == "blocked", s == "pending — user":
		return '/', true
	case s == "merged", strings.HasPrefix(s, "merged "):
		return 'x', true
	case s == "dropped":
		return '-', true
	}
	return '/', false
}

func taskLine(c []string) (string, string) {
	box, known := boxFor(c[5])
	warn := ""
	if !known {
		warn = fmt.Sprintf("unknown status %q, converted as in progress", c[5])
	}
	return fmt.Sprintf("- [%c] %s · %s · %s · %s · %s · deps %s · %s",
		box, orDash(c[0]), noDot(c[5]), orDash(c[1]), orDash(c[2]), waveLabel(c[3]), orDash(c[4]), noDot(c[6])), warn
}

func reviewLine(c []string) string {
	return fmt.Sprintf("- %s · %s · %s · M/S/C %s · %s · fix tasks %s",
		waveLabel(c[0]), orDash(c[1]), orDash(c[2]), orDash(c[3]), orDash(c[4]), orDash(c[5]))
}

// ConvertBoard turns the v1 Tasks and Reviews tables of a board into
// checklist lines and bullets, and the comment above each into the v2 one.
func ConvertBoard(in []byte) Converted {
	d := split(in)
	var out []line
	var problems, warnings []Problem
	var cm tracker
	var fence fenceTracker
	for i := 0; i < len(d.lines); i++ {
		l := d.lines[i]
		if !cm.open && fence.line(l.text, i+1) {
			out = append(out, l)
			continue
		}
		if fence.open() || cm.comment(l.text) {
			out = append(out, l)
			continue
		}
		cells, ok := splitRow(l.text)
		tasks, reviews := ok && sameCells(cells, v1Tasks), ok && sameCells(cells, v1Reviews)
		if !tasks && !reviews {
			out = append(out, l)
			continue
		}
		cols, comment := len(v1Reviews), ReviewsComment
		if tasks {
			cols, comment = len(v1Tasks), TasksComment
		}
		if i+1 >= len(d.lines) || !isSeparator(d.lines[i+1].text) {
			problems = append(problems, Problem{i + 1, "table header without a separator row"})
			out = append(out, l)
			continue
		}
		// the comment right above (blank lines between): replaced
		k := len(out) - 1
		for k >= 0 && strings.TrimSpace(out[k].text) == "" {
			k--
		}
		if k >= 0 && strings.HasPrefix(strings.TrimSpace(out[k].text), "<!--") && strings.HasSuffix(strings.TrimSpace(out[k].text), "-->") {
			cr := out[k].cr
			out = out[:k]
			for _, c := range strings.Split(comment, "\n") {
				out = append(out, line{c, cr})
			}
		}
		i++ // the separator
		for i+1 < len(d.lines) {
			row, isRow := splitRow(d.lines[i+1].text)
			if !isRow {
				break
			}
			i++
			if len(row) != cols {
				problems = append(problems, Problem{i + 1, fmt.Sprintf("a row of %d columns, want %d", len(row), cols)})
				continue
			}
			text := ""
			if tasks {
				var w string
				if text, w = taskLine(row); w != "" {
					warnings = append(warnings, Problem{i + 1, w})
				}
			} else {
				text = reviewLine(row)
			}
			out = append(out, line{text, d.lines[i].cr})
		}
	}
	if fence.open() {
		warnings = append(warnings, Problem{fence.start, "a code fence opened here is never closed; the tables after it were not converted"})
	}
	return d.result(out, in, problems, warnings)
}

// fenceTracker follows Markdown code fences: one closes only with the same
// character, at least as long as the opening, and nothing else on the line.
type fenceTracker struct {
	char  byte
	n     int
	start int // line of the opening, while open
}

func (f *fenceTracker) open() bool { return f.n > 0 }

// line reports whether text (line number no) is a fence marker.
func (f *fenceTracker) line(text string, no int) bool {
	t := strings.TrimSpace(text)
	if len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return false
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	if n < 3 {
		return false
	}
	if f.open() {
		if t[0] == f.char && n >= f.n && strings.TrimSpace(t[n:]) == "" {
			f.n = 0
			return true
		}
		return false
	}
	f.char, f.n, f.start = t[0], n, no
	return true
}

// ConvertReview turns the lines of a review's Dispositions into checklist
// lines, in the order of the contract, and its v1 comment into the v2 one.
func ConvertReview(in []byte) Converted {
	d := split(in)
	var out []line
	var problems []Problem
	var cm tracker
	inSection := false
	for i, l := range d.lines {
		t := strings.TrimSpace(l.text)
		if !cm.open && strings.HasPrefix(t, "## ") {
			inSection = t == "## Dispositions"
		}
		if cm.comment(l.text) {
			switch {
			case inSection && strings.HasPrefix(t, dispositionsV1a):
				for _, c := range strings.Split(DispositionsComment, "\n") {
					out = append(out, line{c, l.cr})
				}
			case inSection && t == dispositionsV1b:
			default:
				out = append(out, l)
			}
			continue
		}
		if !inSection {
			out = append(out, l)
			continue
		}
		m := dispLine.FindStringSubmatch(l.text)
		if m == nil {
			out = append(out, l)
			continue
		}
		text, ok := dispositionLine(m[1], strings.TrimRight(m[2], " \t"))
		if !ok {
			problems = append(problems, Problem{i + 1, "no known form"})
			out = append(out, l)
			continue
		}
		out = append(out, line{text, l.cr})
	}
	return d.result(out, in, problems, nil)
}

func dispositionLine(id, rest string) (string, bool) {
	switch {
	case rest == "open":
		return "- [ ] " + id + " · open", true
	case strings.HasPrefix(rest, "resolved · kept: "):
		return "- [-] " + id + " · " + strings.TrimPrefix(rest, "resolved · "), true
	case strings.HasPrefix(rest, "resolved · "):
		return "- [x] " + id + " · " + rest, true
	}
	if m := deferredRe.FindStringSubmatch(rest); m != nil {
		return fmt.Sprintf("- [x] %s · deferred → [[%s]] · %s #deferred", id, m[1], orDash(m[2])), true
	}
	return "", false
}

// ConvertBacklog turns the items under Open into checklist lines.
func ConvertBacklog(in []byte) Converted {
	d := split(in)
	var out []line
	var cm tracker
	inOpen := false
	for _, l := range d.lines {
		t := strings.TrimSpace(l.text)
		if !cm.open && strings.HasPrefix(t, "## ") {
			inOpen = t == "## Open"
		}
		if cm.comment(l.text) || !inOpen {
			out = append(out, l)
			continue
		}
		if m := backlogRe.FindStringSubmatch(l.text); m != nil {
			out = append(out, line{"- [ ] " + m[1] + " · " + m[2], l.cr})
			continue
		}
		out = append(out, l)
	}
	return d.result(out, in, nil, nil)
}

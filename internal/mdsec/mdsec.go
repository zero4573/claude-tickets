// Package mdsec parses Markdown into byte-exact sections: the frontmatter
// span and ATX heading sections with spans and line numbers. Nothing is
// rewritten, so callers can read or replace sections by byte offsets.
package mdsec

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Span is a byte range [Start, End).
type Span struct{ Start, End int }

// Section is one heading section (or the preamble, Level 0).
type Section struct {
	Level     int      // 1-6; 0 = preamble (after frontmatter, before the first heading)
	Title     string   // heading text, trimmed, without the #s and trailing #s
	Path      []string // ancestor titles + own title (preamble: empty)
	Heading   Span     // the heading line incl. its newline (preamble: empty span)
	Body      Span     // after the heading line, up to the next heading of ANY level
	Whole     Span     // heading + body + subsections (up to the next heading with level <= Level, or EOF)
	StartLine int      // 1-based, inclusive, of Whole
	EndLine   int
}

// Doc is a parsed document.
type Doc struct {
	Src         []byte
	Frontmatter *Span     // the whole block incl. both --- lines; nil if none
	Sections    []Section // document order, preamble first when non-empty
}

// ErrNotFound is returned by Find when no section matches.
var ErrNotFound = errors.New("section not found")

// AmbiguousError is returned by Find when several sections match.
type AmbiguousError struct {
	Spec       string
	Candidates []Section
}

func (e *AmbiguousError) Error() string {
	var parts []string
	for _, c := range e.Candidates {
		parts = append(parts, fmt.Sprintf("%s (line %d)", strings.Join(c.Path, " > "), c.StartLine))
	}
	return fmt.Sprintf("section %q is ambiguous: %s", e.Spec, strings.Join(parts, "; "))
}

type line struct{ start, end, next int } // end excludes the newline; next is the following line's start

func splitLines(src []byte) []line {
	var out []line
	for i := 0; i < len(src); {
		j := bytes.IndexByte(src[i:], '\n')
		if j < 0 {
			out = append(out, line{i, len(src), len(src)})
			break
		}
		out = append(out, line{i, i + j, i + j + 1})
		i += j + 1
	}
	return out
}

func trimCR(b []byte) []byte { return bytes.TrimSuffix(b, []byte("\r")) }

// fenceOpen reports the fence char and length when s opens a fenced code block.
func fenceOpen(s string) (byte, int, bool) {
	t := strings.TrimLeft(s, " \t")
	if len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return 0, 0, false
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	if t[0] == '`' && strings.Contains(t[n:], "`") {
		return 0, 0, false
	}
	return t[0], n, true
}

func fenceClose(s string, ch byte, n int) bool {
	t := strings.TrimSpace(s)
	if len(t) < n {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] != ch {
			return false
		}
	}
	return true
}

// heading parses an ATX heading line.
func heading(s string) (level int, title string, ok bool) {
	i := 0
	for i < len(s) && i < 4 && s[i] == ' ' {
		i++
	}
	if i > 3 {
		return 0, "", false
	}
	n := 0
	for i+n < len(s) && s[i+n] == '#' {
		n++
	}
	if n < 1 || n > 6 {
		return 0, "", false
	}
	rest := s[i+n:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	rest = strings.TrimSpace(rest)
	// closing sequence: trailing #s preceded by whitespace (or the whole text)
	t := strings.TrimRight(rest, "#")
	if t == "" || t[len(t)-1] == ' ' || t[len(t)-1] == '\t' {
		rest = strings.TrimSpace(t)
	}
	return n, rest, true
}

// Parse splits src into frontmatter and sections.
func Parse(src []byte) *Doc {
	d := &Doc{Src: src}
	lines := splitLines(src)
	first := 0
	if len(lines) > 0 && string(trimCR(src[lines[0].start:lines[0].end])) == "---" {
		for i := 1; i < len(lines); i++ {
			if string(trimCR(src[lines[i].start:lines[i].end])) == "---" {
				d.Frontmatter = &Span{0, lines[i].next}
				first = i + 1
				break
			}
		}
	}
	type head struct {
		level int
		title string
		line  int
	}
	var heads []head
	var fch byte
	fn := 0
	inComment := false
	for i := first; i < len(lines); i++ {
		s := string(trimCR(src[lines[i].start:lines[i].end]))
		if inComment {
			if strings.Contains(s, "-->") {
				inComment = false
			}
			continue
		}
		if fn == 0 {
			if k := strings.Index(noCode(s), "<!--"); k >= 0 && !strings.Contains(s[k+4:], "-->") {
				// a comment left open on this line: headings up to the
				// closing --> are not sections
				if _, _, isHead := heading(s); !isHead {
					inComment = true
					continue
				}
			}
		}
		if fn > 0 {
			if fenceClose(s, fch, fn) {
				fn = 0
			}
			continue
		}
		if c, n, ok := fenceOpen(s); ok {
			fch, fn = c, n
			continue
		}
		if lv, t, ok := heading(s); ok {
			heads = append(heads, head{lv, t, i})
		}
	}
	bodyStart := len(src)
	if first < len(lines) {
		bodyStart = lines[first].start
	} else if d.Frontmatter != nil {
		bodyStart = d.Frontmatter.End
	}
	lineOf := func(off int) int { // 1-based line of byte offset
		return sort.Search(len(lines), func(k int) bool { return lines[k].next > off }) + 1
	}
	endLine := func(sp Span) int {
		if sp.End <= sp.Start {
			return lineOf(sp.Start)
		}
		return lineOf(sp.End - 1)
	}
	preEnd := len(src)
	if len(heads) > 0 {
		preEnd = lines[heads[0].line].start
	}
	if preEnd > bodyStart && len(bytes.TrimSpace(src[bodyStart:preEnd])) > 0 {
		sp := Span{bodyStart, preEnd}
		d.Sections = append(d.Sections, Section{
			Heading: Span{bodyStart, bodyStart}, Body: sp, Whole: sp,
			StartLine: lineOf(sp.Start), EndLine: endLine(sp),
		})
	}
	var stack []head
	for k, h := range heads {
		for len(stack) > 0 && stack[len(stack)-1].level >= h.level {
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, h)
		path := make([]string, len(stack))
		for i, s := range stack {
			path[i] = s.title
		}
		hl := lines[h.line]
		bodyEnd := len(src)
		if k+1 < len(heads) {
			bodyEnd = lines[heads[k+1].line].start
		}
		wholeEnd := len(src)
		for _, n := range heads[k+1:] {
			if n.level <= h.level {
				wholeEnd = lines[n.line].start
				break
			}
		}
		whole := Span{hl.start, wholeEnd}
		d.Sections = append(d.Sections, Section{
			Level: h.level, Title: h.title, Path: path,
			Heading: Span{hl.start, hl.next}, Body: Span{hl.next, bodyEnd}, Whole: whole,
			StartLine: h.line + 1, EndLine: endLine(whole),
		})
	}
	return d
}

// Find resolves a section spec: "Title", "## Title" (level-checked) or "A > B" (path suffix).
// Exact match after trimming; 0 matches gives ErrNotFound, more than 1 an *AmbiguousError.
func (d *Doc) Find(spec string) (*Section, error) {
	s := strings.TrimSpace(spec)
	level := 0
	if n := len(s) - len(strings.TrimLeft(s, "#")); n >= 1 && n <= 6 && len(s) > n && (s[n] == ' ' || s[n] == '\t') {
		level = n
		s = strings.TrimSpace(s[n:])
	}
	if strings.Contains(s, ">") {
		// a title may itself contain ">": an exact whole-title match wins
		// over reading the spec as a path
		if hits := d.match(level, []string{s}); len(hits) > 0 {
			return d.pick(spec, hits)
		}
	}
	var want []string
	for _, p := range strings.Split(s, ">") {
		want = append(want, strings.TrimSpace(p))
	}
	return d.pick(spec, d.match(level, want))
}

// match is the indexes of the sections whose path ends with want.
func (d *Doc) match(level int, want []string) []int {
	var hits []int
	for i, sec := range d.Sections {
		if sec.Level == 0 || (level != 0 && sec.Level != level) || len(sec.Path) < len(want) {
			continue
		}
		tail := sec.Path[len(sec.Path)-len(want):]
		match := true
		for j := range want {
			if tail[j] != want[j] {
				match = false
				break
			}
		}
		if match {
			hits = append(hits, i)
		}
	}
	return hits
}

func (d *Doc) pick(spec string, hits []int) (*Section, error) {
	switch len(hits) {
	case 0:
		return nil, ErrNotFound
	case 1:
		return &d.Sections[hits[0]], nil
	}
	e := &AmbiguousError{Spec: spec}
	for _, i := range hits {
		e.Candidates = append(e.Candidates, d.Sections[i])
	}
	return nil, e
}

// noCode blanks backtick code spans (same length), so a literal "<!--" in
// one doesn't open a comment.
func noCode(s string) string {
	if !strings.Contains(s, "`") {
		return s
	}
	b := []byte(s)
	run := func(i int) int {
		n := 0
		for i+n < len(b) && b[i+n] == '`' {
			n++
		}
		return n
	}
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		n := run(i)
		end := -1
		for j := i + n; j < len(b); {
			if b[j] != '`' {
				j++
				continue
			}
			m := run(j)
			if m == n {
				end = j
				break
			}
			j += m
		}
		if end < 0 {
			i += n
			continue
		}
		for k := i; k < end+n; k++ {
			b[k] = ' '
		}
		i = end + n
	}
	return string(b)
}

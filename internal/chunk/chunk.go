// Package chunk splits Markdown into semantic blocks for embedding: one
// chunk per section body, long bodies split at paragraphs, then list items,
// then lines. Spans map every chunk back to the source bytes.
package chunk

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/zero4573/claude-tickets/internal/mdsec"
)

// DefaultMaxTokens is the chunk size limit when Options.MaxTokens is 0.
const DefaultMaxTokens = 256

// Options tunes Split.
type Options struct{ MaxTokens int }

// Chunk is one embeddable block.
type Chunk struct {
	Heading            []string // section Path
	Text               string   // section body text, cleaned
	Start, End         int      // byte span in the source
	StartLine, EndLine int
	Tokens             int // Tokens(Text)
}

// Tokens is the shared token estimate.
func Tokens(s string) int { return (len(s) + 3) / 4 }

type ln struct {
	start, end int    // raw byte span of the line, without newline (and without a trailing \r)
	text       string // cleaned text
	exact      bool   // text is the raw bytes of the span (no comment stripped, no trailing blanks)
}

type unit struct {
	lns   []ln
	sep   string // joiner before this unit when packed after another
	fence bool
}

func (u unit) text() string {
	parts := make([]string, len(u.lns))
	for i, l := range u.lns {
		parts[i] = l.text
	}
	return strings.Join(parts, "\n")
}

// Split chunks src.
func Split(src []byte, o Options) []Chunk {
	max := o.MaxTokens
	if max <= 0 {
		max = DefaultMaxTokens
	}
	doc := mdsec.Parse(src)
	starts := lineStarts(src)
	lineOf := func(off int) int { return sort.Search(len(starts), func(k int) bool { return starts[k] > off }) }
	var out []Chunk
	for _, sec := range doc.Sections {
		for _, group := range pack(units(src, sec.Body), max) {
			var texts []string
			for i, u := range group {
				if i > 0 {
					texts = append(texts, u.sep)
				}
				texts = append(texts, u.text())
			}
			text := strings.Join(texts, "")
			first, last := group[0].lns[0], group[len(group)-1].lns
			end := last[len(last)-1].end
			out = append(out, Chunk{
				Heading: append([]string(nil), sec.Path...), Text: text,
				Start: first.start, End: end,
				StartLine: lineOf(first.start), EndLine: lineOf(maxInt(end-1, first.start)),
				Tokens: Tokens(text),
			})
		}
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func lineStarts(src []byte) []int {
	st := []int{0}
	for i, b := range src {
		if b == '\n' && i+1 < len(src) {
			st = append(st, i+1)
		}
	}
	return st
}

// units turns a body into paragraph and fenced-code units (cleaned lines,
// raw spans), dropping HTML comments and blank lines.
func units(src []byte, body mdsec.Span) []unit {
	var us []unit
	var para []ln
	flush := func() {
		if len(para) > 0 {
			us = append(us, unit{lns: para, sep: "\n\n"})
			para = nil
		}
	}
	var code []ln
	var fch byte
	fn := 0
	inComment := false
	for pos := body.Start; pos < body.End; {
		end := body.End
		next := body.End
		if j := strings.IndexByte(string(src[pos:body.End]), '\n'); j >= 0 {
			end, next = pos+j, pos+j+1
		}
		raw := string(src[pos:end])
		if strings.HasSuffix(raw, "\r") {
			raw = raw[:len(raw)-1]
			end--
		}
		l := ln{start: pos, end: end, text: raw, exact: true}
		pos = next
		if fn > 0 {
			code = append(code, l)
			if fenceClose(raw, fch, fn) {
				us = append(us, unit{lns: code, sep: "\n\n", fence: true})
				code, fn = nil, 0
			}
			continue
		}
		if !inComment {
			if c, n, ok := fenceOpen(raw); ok {
				flush()
				fch, fn = c, n
				code = []ln{l}
				continue
			}
		}
		l.text, inComment = stripComments(raw, inComment)
		l.text = cleanLine(l.text)
		l.exact = l.text == raw
		if strings.TrimSpace(l.text) == "" {
			flush()
			continue
		}
		para = append(para, l)
	}
	if len(code) > 0 { // unterminated fence runs to EOF
		us = append(us, unit{lns: code, sep: "\n\n", fence: true})
	}
	flush()
	return us
}

// cleanLine trims trailing whitespace; wikilinks and embeds keep their text.
func cleanLine(s string) string { return strings.TrimRight(s, " \t") }

func stripComments(s string, in bool) (string, bool) {
	var b strings.Builder
	for {
		if in {
			i := strings.Index(s, "-->")
			if i < 0 {
				return b.String(), true
			}
			s, in = s[i+3:], false
			continue
		}
		i := strings.Index(s, "<!--")
		if i < 0 {
			b.WriteString(s)
			return b.String(), false
		}
		b.WriteString(s[:i])
		s, in = s[i+4:], true
	}
}

// The fence helpers below duplicate the fence rules of internal/mdsec (which
// owns them; this package keeps its own copy): keep both in step.
func fenceOpen(s string) (byte, int, bool) {
	t := strings.TrimLeft(s, " \t")
	if len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return 0, 0, false
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	if n < 3 || (t[0] == '`' && strings.Contains(t[n:], "`")) {
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

func isItem(s string) bool {
	if s == "" || s[0] == ' ' || s[0] == '\t' {
		return false
	}
	if (s[0] == '-' || s[0] == '*' || s[0] == '+') && len(s) > 1 && s[1] == ' ' {
		return true
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(s) && (s[i] == '.' || s[i] == ')') && s[i+1] == ' '
}

// pieces splits a unit that is over max: paragraphs at top-level list items,
// then anything still too big (and big code blocks) by lines.
func pieces(u unit, max int) []unit {
	if Tokens(u.text()) <= max {
		return []unit{u}
	}
	var parts [][]ln
	if !u.fence {
		var cur []ln
		for _, l := range u.lns {
			if isItem(l.text) && len(cur) > 0 {
				parts = append(parts, cur)
				cur = nil
			}
			cur = append(cur, l)
		}
		parts = append(parts, cur)
	} else {
		parts = [][]ln{u.lns}
	}
	var out []unit
	for _, p := range parts {
		pu := unit{lns: p, fence: u.fence}
		if Tokens(pu.text()) <= max {
			out = append(out, pu)
			continue
		}
		var cur []ln
		size := 0
		var plines []ln
		for _, l := range p {
			plines = append(plines, splitLine(l, max)...)
		}
		for _, l := range plines { // by lines, packed up to max
			add := Tokens(l.text) + 1
			if len(cur) > 0 && size+add > max {
				out = append(out, unit{lns: cur, fence: u.fence})
				cur, size = nil, 0
			}
			cur = append(cur, l)
			size += add
		}
		out = append(out, unit{lns: cur, fence: u.fence})
	}
	for i := range out {
		out[i].sep = "\n"
		if i == 0 {
			out[i].sep = u.sep
		}
	}
	return out
}

// pack greedily groups units into chunks of at most max tokens.
func pack(us []unit, max int) [][]unit {
	var groups [][]unit
	var cur []unit
	curText := ""
	for _, u := range us {
		for _, p := range pieces(u, max) {
			if len(cur) > 0 {
				if Tokens(curText+p.sep+p.text()) > max {
					groups = append(groups, cur)
					cur, curText = nil, ""
				} else {
					curText += p.sep + p.text()
					cur = append(cur, p)
					continue
				}
			}
			cur = []unit{p}
			curText = p.text()
		}
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return groups
}

// splitLine breaks a line over max tokens at word boundaries (a word longer
// than max is cut at a rune boundary). The parts keep exact byte spans when
// the line's text is its raw bytes, else the whole line's span.
func splitLine(l ln, max int) []ln {
	if Tokens(l.text) <= max {
		return []ln{l}
	}
	limit := max * 4
	var out []ln
	text := l.text
	off := 0 // offset in the line text
	emit := func(from, to int) {
		part := ln{start: l.start, end: l.end, text: text[from:to]}
		if l.exact {
			part.start, part.end, part.exact = l.start+from, l.start+to, true
		}
		out = append(out, part)
	}
	for off < len(text) {
		if len(text)-off <= limit {
			emit(off, len(text))
			break
		}
		cut := off + limit
		if i := strings.LastIndexAny(text[off:cut+1], " \t"); i > 0 {
			cut = off + i
		} else {
			for cut > off && !utf8.RuneStart(text[cut]) {
				cut--
			}
		}
		emit(off, cut)
		off = cut
		for off < len(text) && (text[off] == ' ' || text[off] == '\t') {
			off++
		}
	}
	return out
}

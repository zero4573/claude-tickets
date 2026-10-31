// Package cite reads the code citations of a vault note and checks them
// against the pinned commits of the repos they cite: that the file exists
// there, that the cited lines are inside it, that a captioned code chunk is
// the file's text at those lines, and optionally that the caption's symbols
// appear in them.
//
// A note cites code in two ways:
//
//   - a caption line above a fenced code block (the chunk):
//     **<repo>** · `<path>` · `<Symbol>`, … · lines <N>-<M>
//   - an inline reference in backticks, outside code blocks:
//     `<path>:<N>`, `<path>:<N>-<M>`, or `<repo>:<path>:<N>[-<M>]`
//
// and states the commit each repo is read at as <repo> `<branch>` @ `<sha>`
// (the branch is optional).
package cite

import (
	"regexp"
	"strconv"
	"strings"
)

// Citation is one cited range of one file.
type Citation struct {
	Line    int    // 1-based line of the note
	Text    string // the citation as written (a caption line, or the backticked reference)
	Repo    string // the repo as the note names it, "" when it names none
	Path    string
	Start   int
	End     int
	Caption bool     // a caption line, not an inline reference
	Symbols []string // a caption's backticked symbols
	Chunk   []string // a caption's code block (its lines), nil when none follows
}

// Note is what Parse finds in a note.
type Note struct {
	Citations []Citation
	// Pins maps a repo's Key to the commit the note states for it.
	Pins map[string]string
}

var (
	captionRe = regexp.MustCompile("^\\*\\*([^*]+)\\*\\*\\s*·\\s*`([^`]+)`(.*?)·\\s*lines?\\s+([0-9]+)(?:\\s*[-–]\\s*([0-9]+))?\\s*$")
	spanRe    = regexp.MustCompile("`([^`\n]+)`")
	inlineRe  = regexp.MustCompile(`^(?:([A-Za-z0-9][A-Za-z0-9_.-]*):)?([^\s:]+):([0-9]+)(?:[-–]([0-9]+))?$`)
	pinRe     = regexp.MustCompile("@\\s*`([0-9a-f]{7,64})`")
	extRe     = regexp.MustCompile(`\.[A-Za-z][A-Za-z0-9]{0,4}$`)
)

// Key is how repo names are compared: lower case, `_` as `-`.
func Key(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "_", "-")
}

// fence is the opening fence of a code block on line l, if it is one.
func fence(l string) (marker string, indent int, ok bool) {
	t := strings.TrimLeft(l, " ")
	indent = len(l) - len(t)
	if indent > 3 {
		return "", 0, false
	}
	for _, c := range []string{"`", "~"} {
		n := len(t) - len(strings.TrimLeft(t, c))
		if n >= 3 {
			return strings.Repeat(c, n), indent, true
		}
	}
	return "", 0, false
}

// closes says whether l closes a block opened with marker.
func closes(l, marker string) bool {
	t := strings.TrimSpace(l)
	return strings.HasPrefix(t, marker) && strings.Trim(t, marker[:1]) == ""
}

// Parse reads a note's citations and pins (a frontmatter block is
// skipped; code blocks hold no inline references or pins).
func Parse(src string) Note {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	n := Note{Pins: map[string]string{}}
	i := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for j := 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "---" {
				i = j + 1
				break
			}
		}
	}
	for ; i < len(lines); i++ {
		l := lines[i]
		if marker, _, ok := fence(l); ok {
			// a block not claimed by a caption: skip it whole
			for i++; i < len(lines) && !closes(lines[i], marker); i++ {
			}
			continue
		}
		if m := captionRe.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			c := Citation{Line: i + 1, Text: strings.TrimSpace(l), Repo: strings.TrimSpace(m[1]),
				Path: m[2], Caption: true}
			c.Start, _ = strconv.Atoi(m[4])
			c.End = c.Start
			if m[5] != "" {
				c.End, _ = strconv.Atoi(m[5])
			}
			for _, s := range spanRe.FindAllStringSubmatch(m[3], -1) {
				c.Symbols = append(c.Symbols, s[1])
			}
			// the chunk: the code block after the caption (blank lines between)
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			if j < len(lines) {
				if marker, indent, ok := fence(lines[j]); ok {
					c.Chunk = []string{}
					for j++; j < len(lines) && !closes(lines[j], marker); j++ {
						c.Chunk = append(c.Chunk, dedent(lines[j], indent))
					}
					i = j
				}
			}
			n.Citations = append(n.Citations, c)
			continue
		}
		pins(l, n.Pins)
		for _, s := range spanRe.FindAllStringSubmatch(l, -1) {
			if c, ok := inline(s[1]); ok {
				c.Line = i + 1
				n.Citations = append(n.Citations, c)
			}
		}
	}
	return n
}

// dedent drops up to indent leading spaces (a fence indented in a list
// indents its content as much).
func dedent(l string, indent int) string {
	for k := 0; k < indent && strings.HasPrefix(l, " "); k++ {
		l = l[1:]
	}
	return l
}

// inline reads a backticked reference; only one whose path looks like a
// file (a `/`, or a short extension) counts, so `localhost:8080` or an IP
// address is no citation.
func inline(s string) (Citation, bool) {
	m := inlineRe.FindStringSubmatch(s)
	if m == nil || strings.Contains(s, "://") {
		return Citation{}, false
	}
	p := m[2]
	if !strings.Contains(p, "/") && !extRe.MatchString(p) {
		return Citation{}, false
	}
	c := Citation{Text: "`" + s + "`", Repo: m[1], Path: p}
	c.Start, _ = strconv.Atoi(m[3])
	c.End = c.Start
	if m[4] != "" {
		c.End, _ = strconv.Atoi(m[4])
	}
	return c, true
}

// pins adds each <repo> [`<branch>`] @ `<sha>` of the line.
func pins(l string, into map[string]string) {
	for _, m := range pinRe.FindAllStringSubmatchIndex(l, -1) {
		before := strings.TrimRight(l[:m[0]], " ")
		if strings.HasSuffix(before, "`") {
			if k := strings.LastIndex(before[:len(before)-1], "`"); k >= 0 {
				before = strings.TrimRight(before[:k], " ")
			}
		}
		name := before
		if k := strings.LastIndexAny(name, " \t"); k >= 0 {
			name = name[k+1:]
		}
		name = strings.Trim(name, "*`[]:,;(")
		if k := strings.LastIndex(name, "|"); k >= 0 {
			name = name[k+1:]
		}
		if name = Key(name); name != "" {
			into[name] = l[m[2]:m[3]]
		}
	}
}

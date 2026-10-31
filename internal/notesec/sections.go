// Package notesec reads and edits a Markdown note by section and by
// frontmatter field, changing only the bytes of the span it edits, and
// applies the write rules that keep ticket agents inside their own area.
package notesec

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/zero4573/tix/internal/chunk"
	"github.com/zero4573/tix/internal/mdsec"
)

// OutlineEntry is one section of a note's outline.
type OutlineEntry struct {
	Level     int      `json:"level"`
	Title     string   `json:"title"`
	Path      []string `json:"path"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Tokens    int      `json:"tokens"`
}

// Lines is a 1-based inclusive line range.
type Lines struct {
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

// Outline is a note's frontmatter span and its sections (the preamble is
// not listed: it has no heading).
type Outline struct {
	Frontmatter *Lines         `json:"frontmatter"`
	Sections    []OutlineEntry `json:"sections"`
}

// OutlineOf lists the structure of src.
func OutlineOf(src []byte) Outline {
	d := mdsec.Parse(src)
	o := Outline{Sections: []OutlineEntry{}}
	if d.Frontmatter != nil {
		o.Frontmatter = &Lines{1, 1 + bytes.Count(src[:d.Frontmatter.End-1], []byte("\n"))}
	}
	for _, s := range d.Sections {
		if s.Level == 0 {
			continue
		}
		o.Sections = append(o.Sections, OutlineEntry{
			Level: s.Level, Title: s.Title, Path: s.Path,
			StartLine: s.StartLine, EndLine: s.EndLine,
			Tokens: chunk.Tokens(string(src[s.Whole.Start:s.Whole.End])),
		})
	}
	return o
}

// Get is the bytes of a section (all of src for an empty spec). Without
// subsections it stops at the first subsection heading.
func Get(src []byte, spec string, noSub bool) ([]byte, error) {
	if strings.TrimSpace(spec) == "" {
		return src, nil
	}
	s, err := mdsec.Parse(src).Find(spec)
	if err != nil {
		return nil, err
	}
	end := s.Whole.End
	if noSub {
		end = s.Body.End
	}
	return src[s.Whole.Start:end], nil
}

func crlf(src []byte) bool { return bytes.Contains(src, []byte("\r\n")) }

// fit gives content the note's line endings and exactly one final newline.
func fit(content []byte, cr bool) []byte {
	c := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	c = append(bytes.TrimRight(c, "\n"), '\n')
	if cr {
		c = bytes.ReplaceAll(c, []byte("\n"), []byte("\r\n"))
	}
	return c
}

func blank(b []byte) bool { return len(bytes.TrimSpace(b)) == 0 }

// hasBlankTail reports a blank line at the end of b (b ends with "\n\n").
func hasBlankTail(b []byte) bool {
	b = bytes.TrimSuffix(b, []byte("\n"))
	b = bytes.TrimSuffix(b, []byte("\r"))
	if len(b) == 0 {
		return false
	}
	i := bytes.LastIndexByte(b, '\n') + 1
	return blank(b[i:])
}

func hasBlankHead(b []byte) bool {
	i := bytes.IndexByte(b, '\n')
	return i >= 0 && blank(b[:i])
}

// SetSection replaces a section's body (not its heading line): up to its
// first subsection, or through the subsections with withSub. Everything
// outside the replaced span stays as it was.
func SetSection(src []byte, spec string, content []byte, withSub bool) ([]byte, Target, error) {
	s, err := mdsec.Parse(src).Find(spec)
	if err != nil {
		return nil, Target{}, err
	}
	t := Target{Section: s.Path, Level: s.Level, Content: content}
	start, end := s.Body.Start, s.Body.End
	if withSub {
		end = s.Whole.End
	}
	old := src[start:end]
	var nb []byte
	if s.Heading.End == len(src) && !bytes.HasSuffix(src, []byte("\n")) {
		nb = append(nb, nlOf(src)...) // heading was the last line, unterminated
	}
	body := fit(content, crlf(src))
	if blank(content) {
		body = nil
	} else if hasBlankHead(old) && !hasBlankHead(body) {
		nb = append(nb, nlOf(src)...)
	}
	nb = append(nb, body...)
	if end < len(src) && hasBlankTail(old) && !blank(body) {
		nb = append(nb, nlOf(src)...)
	}
	return splice(src, start, end, nb), t, nil
}

func splice(src []byte, start, end int, repl []byte) []byte {
	out := make([]byte, 0, len(src)-(end-start)+len(repl))
	out = append(out, src[:start]...)
	out = append(out, repl...)
	return append(out, src[end:]...)
}

// Append adds content at the end of a section's own body (before its first
// subsection). With create > 0 a missing section is added as a level-create
// heading at the end of the note.
func Append(src []byte, spec string, content []byte, create int) ([]byte, Target, error) {
	d := mdsec.Parse(src)
	s, err := d.Find(spec)
	cr := crlf(src)
	nl := "\n"
	if cr {
		nl = "\r\n"
	}
	if errors.Is(err, mdsec.ErrNotFound) && create > 0 {
		if create > 6 {
			return nil, Target{}, fmt.Errorf("--create %d: a heading level is 1 to 6", create)
		}
		title := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(spec), "#"))
		if strings.Contains(title, ">") || title == "" {
			return nil, Target{}, fmt.Errorf("--create needs a plain section title, not %q", spec)
		}
		var add []byte
		if len(src) > 0 && src[len(src)-1] != '\n' {
			add = append(add, nl...)
		}
		if len(src) > 0 && !hasBlankTail(append(append([]byte{}, src...), add...)) {
			add = append(add, nl...)
		}
		add = append(add, strings.Repeat("#", create)+" "+title+nl+nl...)
		add = append(add, fit(content, cr)...)
		return append(append([]byte{}, src...), add...), Target{Section: []string{title}, Level: create, Content: content}, nil
	}
	if err != nil {
		return nil, Target{}, err
	}
	t := Target{Section: s.Path, Level: s.Level, Content: content}
	body := src[s.Body.Start:s.Body.End]
	var nb []byte
	if s.Heading.End == len(src) && !bytes.HasSuffix(src, []byte("\n")) {
		return append(append(append([]byte{}, src...), nl...), fit(content, cr)...), t, nil
	}
	if blank(body) {
		nb = fit(content, cr)
		if len(body) > 0 && s.Body.End < len(src) {
			nb = append(nb, nl...)
		}
		return splice(src, s.Body.Start, s.Body.End, append(append([]byte{}, body...), nb...)), t, nil
	}
	// before the trailing blank lines
	cut := len(bytes.TrimRight(body, " \t\r\n"))
	if j := bytes.IndexByte(body[cut:], '\n'); j >= 0 {
		cut += j + 1
	} else {
		cut = len(body)
	}
	if cut == len(body) && !bytes.HasSuffix(body, []byte("\n")) {
		nb = append(nb, nl...)
	}
	nb = append(nb, fit(content, cr)...)
	at := s.Body.Start + cut
	return splice(src, at, at, nb), t, nil
}

func nlOf(src []byte) string {
	if crlf(src) {
		return "\r\n"
	}
	return "\n"
}

// Target says what a write touches, for the write rules: a section (its
// heading path) or a frontmatter field.
type Target struct {
	Section []string
	Field   string
	Level   int    // the section's heading level (0: unknown)
	Content []byte // Markdown written into the section, for the heading check
}

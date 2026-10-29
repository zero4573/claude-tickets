// Package spec reads specs: a capability's spec.md (requirement blocks,
// scenarios, clarification markers), the "## Spec change" section of a
// ticket description, the IDs used for the duplicate check, and where a
// repo's specs live. Parsing never panics: a malformed block is reported
// as a Problem with file:line and the rest of the file is still read.
package spec

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Status of a requirement.
const (
	Proposed    = "proposed"
	Implemented = "implemented"
	Inferred    = "inferred"
)

var (
	idRe      = regexp.MustCompile(`^REQ-([A-Z][A-Z0-9]{1,7})-([1-9][0-9]*)$`)
	prefixRe  = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,7}$`)
	headingRe = regexp.MustCompile(`^(#{1,6})[ \t]+(.*?)[ \t]*$`)
	reqHeadRe = regexp.MustCompile(`^REQ-\S+`)
	shallRe   = regexp.MustCompile(`\bSHALL\b`)
	markerRe  = regexp.MustCompile(`^\[NEEDS CLARIFICATION:\s*(.*?)\s*\]$`)
	stepRe    = regexp.MustCompile(`^-\s+(WHEN|THEN|AND)\s+(.+?)\s*$`)
)

// Problem is a malformed spot: File:Line and what is wrong.
type Problem struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Msg  string `json:"message"`
}

func (p Problem) String() string { return fmt.Sprintf("%s:%d: %s", p.File, p.Line, p.Msg) }

// Step is one "- WHEN|THEN|AND <text>" line of a scenario.
type Step struct {
	Kind string `json:"kind"` // WHEN, THEN or AND
	Text string `json:"text"`
	Line int    `json:"line"`
}

// Scenario is a "#### Scenario: <name>" block.
type Scenario struct {
	Name  string `json:"name"`
	Line  int    `json:"line"`
	Steps []Step `json:"steps"`
}

// Marker is a [NEEDS CLARIFICATION: <question>] line.
type Marker struct {
	Question string `json:"question"`
	Line     int    `json:"line"`
}

// Requirement is one "### REQ-<PREFIX>-<n> · <title>" block.
type Requirement struct {
	ID        string     `json:"id"`
	Prefix    string     `json:"prefix"`
	Num       int        `json:"num"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Evidence  []string   `json:"evidence,omitempty"` // inferred only
	Tests     []string   `json:"tests,omitempty"`    // inferred only
	Statement string     `json:"statement"`
	Markers   []Marker   `json:"markers,omitempty"`
	Scenarios []Scenario `json:"scenarios,omitempty"`
	// Parts holds the three parts of a bug's change (description and
	// spec-draft only): keys "Current", "Expected", "Unchanged", the text
	// under each "#### <part>" heading.
	Parts map[string]string `json:"parts,omitempty"`
	Line  int               `json:"line"`
}

// Open reports whether the requirement has a clarification marker, which
// keeps it from being filed in a ticket or planned.
func (r Requirement) Open() bool { return len(r.Markers) > 0 }

// Spec is a parsed spec.md.
type Spec struct {
	File         string        `json:"file"`
	Capability   string        `json:"capability"`
	Prefix       string        `json:"prefix"`
	Requirements []Requirement `json:"requirements"`
	Problems     []Problem     `json:"problems,omitempty"`
}

// ParseFile reads and parses a spec.md. The error is only for an
// unreadable file; format problems are in Spec.Problems.
func ParseFile(file string) (*Spec, error) {
	b, err := readSpec(file)
	if err != nil {
		return nil, err
	}
	return Parse(file, b), nil
}

// MaxSpecSize caps a spec.md that ct reads: the parser runs on the host
// over files a session can write.
const MaxSpecSize = 1 << 20

// readSpec reads a spec.md that is a regular file (not a symlink, which
// could lead out of the repo) of at most MaxSpecSize bytes.
func readSpec(file string) ([]byte, error) {
	st, err := os.Lstat(file)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", file)
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxSpecSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxSpecSize {
		return nil, fmt.Errorf("%s: larger than %d bytes", file, MaxSpecSize)
	}
	return b, nil
}

// Parse parses a spec.md: frontmatter (capability, prefix) and the
// requirement blocks under "## Requirements". name is used in problems.
func Parse(name string, src []byte) *Spec {
	lines := splitLines(src)
	s := &Spec{File: name}
	body := 0
	fm, end, ok := frontmatter(lines)
	if ok {
		body = end
		s.Capability, s.Prefix = fm["capability"], fm["prefix"]
	}
	if s.Capability == "" {
		s.problem(1, "missing frontmatter field capability")
	}
	if !prefixRe.MatchString(s.Prefix) {
		s.problem(1, "frontmatter prefix must be 2-8 capitals or digits starting with a capital, got %q", s.Prefix)
	}
	// Requirements sit under "## Requirements" (level 2) until the next ## heading.
	inReqs, found := false, false
	var blocks [][2]int
	start := -1
	closeBlock := func(to int) {
		if start >= 0 {
			blocks = append(blocks, [2]int{start, to})
			start = -1
		}
	}
	fence := false
	for i := body; i < len(lines); i++ {
		if isFence(lines[i]) {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		lvl, text := heading(lines[i])
		switch {
		case lvl == 2:
			closeBlock(i)
			inReqs = text == "Requirements"
			found = found || inReqs
		case lvl == 3 && inReqs:
			closeBlock(i)
			start = i
		case lvl == 1:
			closeBlock(i)
			inReqs = false
		}
	}
	closeBlock(len(lines))
	if !found {
		s.problem(1, "missing ## Requirements section")
	}
	seen := map[string]int{}
	for _, b := range blocks {
		r, probs := parseReq(name, lines, b[0], b[1])
		s.Problems = append(s.Problems, probs...)
		if r == nil {
			continue
		}
		if r.Prefix != s.Prefix && prefixRe.MatchString(s.Prefix) {
			s.problem(r.Line, "%s does not use the spec's prefix %s", r.ID, s.Prefix)
		}
		if first, dup := seen[r.ID]; dup {
			s.problem(r.Line, "duplicate ID %s (first at line %d)", r.ID, first)
		}
		seen[r.ID] = r.Line
		s.Requirements = append(s.Requirements, *r)
	}
	return s
}

func (s *Spec) problem(line int, f string, a ...any) {
	s.Problems = append(s.Problems, Problem{File: s.File, Line: line, Msg: fmt.Sprintf(f, a...)})
}

// parseReq parses lines[from:to], from being the "###" heading.
func parseReq(file string, lines []string, from, to int) (*Requirement, []Problem) {
	var probs []Problem
	bad := func(line int, f string, a ...any) {
		probs = append(probs, Problem{File: file, Line: line + 1, Msg: fmt.Sprintf(f, a...)})
	}
	_, text := heading(lines[from])
	if !reqHeadRe.MatchString(text) {
		bad(from, "heading under ## Requirements is not a requirement (REQ-<PREFIX>-<n> · <title>): %q", text)
		return nil, probs
	}
	idPart, title, hasSep := strings.Cut(text, "·")
	idPart, title = strings.TrimSpace(idPart), strings.TrimSpace(title)
	m := idRe.FindStringSubmatch(idPart)
	if m == nil {
		bad(from, "malformed requirement ID %q (want REQ-[A-Z][A-Z0-9]{1,7}-[1-9][0-9]*)", idPart)
		return nil, probs
	}
	r := &Requirement{ID: idPart, Prefix: m[1], Line: from + 1, Title: title}
	r.Num, _ = strconv.Atoi(m[2])
	if !hasSep || title == "" {
		bad(from, "%s: heading needs \" · <title>\"", r.ID)
	}
	i := from + 1
	// status: the line right after the heading
	if i < to {
		if k, v, ok := keyLine(lines[i]); ok && k == "status" {
			switch v {
			case Proposed, Implemented, Inferred:
				r.Status = v
			default:
				bad(i, "%s: status must be proposed, implemented or inferred, got %q", r.ID, v)
			}
			i++
		} else {
			bad(i, "%s: the line after the heading must be \"status: proposed|implemented|inferred\"", r.ID)
		}
	} else {
		bad(from, "%s: missing status line", r.ID)
	}
	// evidence / tests (inferred only), then the statement
	for ; i < to; i++ {
		k, v, ok := keyLine(lines[i])
		if !ok || (k != "evidence" && k != "tests") {
			break
		}
		if r.Status != Inferred && r.Status != "" {
			bad(i, "%s: %s is for inferred requirements only", r.ID, k)
		}
		list := splitList(v)
		if k == "evidence" {
			r.Evidence = list
		} else {
			r.Tests = list
		}
	}
	var stmt []string
	var cur *Scenario
	var part string
	var partLines []string
	flushPart := func() {
		if part != "" {
			if r.Parts == nil {
				r.Parts = map[string]string{}
			}
			r.Parts[part] = strings.TrimSpace(strings.Join(partLines, "\n"))
		}
		part, partLines = "", nil
	}
	fence := false
	for ; i < to; i++ {
		ln := lines[i]
		if isFence(ln) {
			fence = !fence
		}
		if !fence {
			if lvl, h := heading(ln); lvl == 4 {
				flushPart()
				cur = nil
				switch {
				case strings.HasPrefix(h, "Scenario:"):
					name := strings.TrimSpace(strings.TrimPrefix(h, "Scenario:"))
					if name == "" {
						bad(i, "%s: scenario without a name", r.ID)
					}
					r.Scenarios = append(r.Scenarios, Scenario{Name: name, Line: i + 1})
					cur = &r.Scenarios[len(r.Scenarios)-1]
				case h == "Current" || h == "Expected" || h == "Unchanged":
					part = h
				case h == "Interfaces":
					bad(i, "%s: #### Interfaces belongs after the requirement blocks", r.ID)
				default:
					bad(i, "%s: unknown #### heading %q (Scenario: <name>, Current, Expected or Unchanged)", r.ID, h)
				}
				continue
			}
		}
		t := strings.TrimSpace(ln)
		if part != "" {
			partLines = append(partLines, ln)
			continue
		}
		if cur != nil {
			if t == "" {
				continue
			}
			if sm := stepRe.FindStringSubmatch(t); sm != nil {
				cur.Steps = append(cur.Steps, Step{Kind: sm[1], Text: sm[2], Line: i + 1})
			} else {
				bad(i, "%s: scenario %q: line is not \"- WHEN|THEN|AND <text>\"", r.ID, cur.Name)
			}
			continue
		}
		if mm := markerRe.FindStringSubmatch(t); mm != nil {
			r.Markers = append(r.Markers, Marker{Question: mm[1], Line: i + 1})
			continue
		}
		if strings.HasPrefix(t, "[NEEDS CLARIFICATION") {
			bad(i, "%s: malformed clarification marker (want [NEEDS CLARIFICATION: <question>] on its own line)", r.ID)
			continue
		}
		stmt = append(stmt, ln)
	}
	flushPart()
	r.Statement = strings.TrimSpace(strings.Join(stmt, "\n"))
	if r.Statement == "" {
		bad(from, "%s: missing statement", r.ID)
	} else if !shallRe.MatchString(r.Statement) {
		bad(from, "%s: statement has no SHALL", r.ID)
	}
	if r.Parts == nil {
		checkSteps(r, bad, from)
	}
	return r, probs
}

func checkSteps(r *Requirement, bad func(int, string, ...any), from int) {
	if len(r.Scenarios) == 0 {
		bad(from, "%s: needs at least one #### Scenario", r.ID)
	}
	for _, sc := range r.Scenarios {
		// WHEN, then ANDs, THEN, then ANDs.
		state := 0 // 0 start, 1 in WHEN, 2 in THEN
		okOrder := true
		for _, st := range sc.Steps {
			switch st.Kind {
			case "WHEN":
				okOrder = okOrder && state == 0
				state = 1
			case "THEN":
				okOrder = okOrder && state == 1
				state = 2
			case "AND":
				okOrder = okOrder && state != 0
			}
		}
		if !okOrder || state != 2 {
			bad(sc.Line-1, "%s: scenario %q must be WHEN, any AND, THEN, any AND", r.ID, sc.Name)
		}
	}
}

func keyLine(l string) (k, v string, ok bool) {
	if i := strings.Index(l, "#"); i >= 0 { // trailing comment, as in the doc's example
		l = l[:i]
	}
	k, v, ok = strings.Cut(l, ":")
	if !ok {
		return "", "", false
	}
	k = strings.TrimSpace(k)
	if k == "" || strings.ContainsAny(k, " \t") {
		return "", "", false
	}
	return k, strings.TrimSpace(v), true
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitLines(src []byte) []string {
	s := strings.TrimPrefix(string(src), "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

func isFence(l string) bool {
	t := strings.TrimSpace(l)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// heading returns the level and text of an ATX heading line, 0 when not one.
func heading(l string) (int, string) {
	m := headingRe.FindStringSubmatch(l)
	if m == nil {
		return 0, ""
	}
	return len(m[1]), m[2]
}

// frontmatter reads a leading --- block; end is the index of the first
// line after it.
func frontmatter(lines []string) (map[string]string, int, bool) {
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return nil, 0, false
	}
	fm := map[string]string{}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			return fm, i + 1, true
		}
		if k, v, ok := strings.Cut(lines[i], ":"); ok {
			if _, dup := fm[k]; !dup {
				fm[strings.TrimSpace(k)] = unquote(strings.TrimSpace(v))
			}
		}
	}
	return fm, len(lines), false // unterminated
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1]
	}
	return v
}

package spec

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Finding kinds of Check.
const (
	KindDuplicate = "duplicate-id"
	KindMalformed = "malformed"
	KindMarker    = "marker"
	KindUntested  = "untested"
	KindInterface = "interface"
)

// Finding is one thing ct spec check reports.
type Finding struct {
	Repo    string `json:"repo"`
	Kind    string `json:"kind"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	ID      string `json:"id,omitempty"`
	Message string `json:"message"`
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", f.File, f.Line, f.Kind, f.Message)
}

// Target is one repo to check: its slug, its checkout (where the tests
// are; empty when there is none, which skips the test-citation check) and
// the root of its specs.
type Target struct {
	Slug  string
	Dir   string
	Specs string
}

// Row is one row of a "### Provides" or "### Consumes" table.
type Row struct {
	Dir                          string // "provides" or "consumes"
	Interface, Kind, Where, With string
	File                         string
	Line                         int
}

var (
	anyIDRe    = regexp.MustCompile(`REQ-[A-Z][A-Z0-9]{1,7}-[0-9]+`)
	testNameRe = regexp.MustCompile(`(^test_|_test\.|\.test\.|\.spec\.|Tests?\.[A-Za-z]+$|^test\.)`)
)

// Limits of the test-citation search, so a huge checkout can't stall it
// (variables for the tests).
var (
	maxTestFiles = 50000
	maxWalk      = 500000
)

// Check analyses the specs of every target and returns the findings of
// those whose slug is in only (all when only is empty), sorted by repo,
// file and line. The other targets are still read: their Interfaces are
// what a repo's provides and consumes rows are matched against. It reads
// files only: no git, no exec.
func Check(targets []Target, only []string) []Finding {
	want := map[string]bool{}
	for _, s := range only {
		want[s] = true
	}
	type loaded struct {
		t     Target
		rows  []Row
		specs []*Spec
		fs    []Finding
	}
	var all []*loaded
	for _, t := range targets {
		l := &loaded{t: t}
		l.specs, l.rows, l.fs = loadTarget(t)
		all = append(all, l)
	}
	rowsBy := map[string][]Row{}
	for _, l := range all {
		if len(l.specs) > 0 {
			rowsBy[l.t.Slug] = l.rows
		}
	}
	var out []Finding
	for _, l := range all {
		if len(want) > 0 && !want[l.t.Slug] {
			continue
		}
		out = append(out, l.fs...)
		for _, r := range l.rows {
			if msg := unmatched(r, l.t.Slug, rowsBy); msg != "" {
				out = append(out, Finding{Repo: l.t.Slug, Kind: KindInterface, File: r.File, Line: r.Line, Message: msg})
			}
		}
		out = append(out, untested(l.t, l.specs)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return out
}

// loadTarget parses the repo's specs: problems, duplicates and markers
// become findings; the interface rows are kept for matching.
func loadTarget(t Target) ([]*Spec, []Row, []Finding) {
	var fs []Finding
	add := func(kind, file string, line int, id, f string, a ...any) {
		fs = append(fs, Finding{Repo: t.Slug, Kind: kind, File: file, Line: line, ID: id, Message: fmt.Sprintf(f, a...)})
	}
	caps, err := Capabilities(t.Specs)
	if err != nil {
		add(KindMalformed, t.Specs, 1, "", "%v", err)
		return nil, nil, fs
	}
	var specs []*Spec
	var rows []Row
	var refs []IDRef
	for _, c := range caps {
		b, err := readSpec(c.File)
		if err != nil {
			add(KindMalformed, c.File, 1, "", "%v", err)
			continue
		}
		s := Parse(c.File, b)
		specs = append(specs, s)
		refs = append(refs, FindIDs(c.File, b)...)
		rows = append(rows, interfaceRows(c.File, b)...)
		for _, p := range s.Problems {
			// duplicates are reported once, repo-wide, below
			if !strings.HasPrefix(p.Msg, "duplicate ID ") {
				add(KindMalformed, p.File, p.Line, "", "%s", p.Msg)
			}
		}
		for _, r := range s.Requirements {
			for _, m := range r.Markers {
				add(KindMarker, c.File, m.Line, r.ID, "%s has an open clarification: %s", r.ID, m.Question)
			}
		}
	}
	dups := Duplicates(refs)
	for _, id := range SortedIDs(dups) {
		d := dups[id]
		for _, r := range d[1:] {
			add(KindDuplicate, r.File, r.Line, id, "duplicate ID %s (first at %s:%d)", id, d[0].File, d[0].Line)
		}
	}
	return specs, rows, fs
}

// interfaceRows reads the Provides and Consumes tables under "## Interfaces".
// Each row carries its direction (Dir).
func interfaceRows(file string, src []byte) []Row {
	var out []Row
	in, dir, fence := false, "", false
	for i, ln := range splitLines(src) {
		if isFence(ln) {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		if lvl, h := heading(ln); lvl > 0 {
			switch {
			case lvl == 2:
				in, dir = h == "Interfaces", ""
			case lvl == 3 && in:
				dir = strings.ToLower(h)
				if dir != "provides" && dir != "consumes" {
					dir = ""
				}
			}
			continue
		}
		t := strings.TrimSpace(ln)
		if dir == "" || !strings.HasPrefix(t, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(t, "|"), "|")
		if len(cells) != 4 {
			continue
		}
		for j := range cells {
			cells[j] = strings.Join(strings.Fields(cells[j]), " ")
		}
		if cells[0] == "Interface" || strings.Trim(cells[0], "-: ") == "" {
			continue
		}
		out = append(out, Row{Interface: cells[0], Kind: cells[1], Where: cells[2], With: cells[3], File: file, Line: i + 1, Dir: dir})
	}
	return out
}

// unmatched says why a row has no counterpart in the other repo's spec
// (a provides row wants a consumes row of the same interface and kind
// naming this repo, and the other way round), "" when it has one.
func unmatched(r Row, slug string, rowsBy map[string][]Row) string {
	if r.With == "" || r.With == "-" {
		return fmt.Sprintf("%s %s names no repo in With", r.Dir, r.Interface)
	}
	other, ok := rowsBy[r.With]
	if !ok {
		return fmt.Sprintf("%s %s: %s has no spec to match it", r.Dir, r.Interface, r.With)
	}
	want := "consumes"
	if r.Dir == "consumes" {
		want = "provides"
	}
	for _, o := range other {
		if o.Dir == want && o.Interface == r.Interface && o.Kind == r.Kind && o.With == slug {
			return ""
		}
	}
	return fmt.Sprintf("%s %s (%s) has no matching %s row in %s", r.Dir, r.Interface, r.Kind, want, r.With)
}

// untested reports the implemented requirements no test file of the
// repo's checkout cites by ID.
func untested(t Target, specs []*Spec) []Finding {
	var impl []Requirement
	var files []string
	for _, s := range specs {
		for _, r := range s.Requirements {
			if r.Status == Implemented {
				impl = append(impl, r)
				files = append(files, s.File)
			}
		}
	}
	if len(impl) == 0 || t.Dir == "" {
		return nil
	}
	cited, capped := citedIDs(t.Dir, t.Specs)
	if capped != "" {
		// what the search didn't reach would read as untested
		return []Finding{{Repo: t.Slug, Kind: KindMalformed, File: t.Dir,
			Message: "the test-citation search stopped at " + capped + ", so untested requirements aren't reported"}}
	}
	var out []Finding
	for i, r := range impl {
		if !cited[r.ID] {
			out = append(out, Finding{Repo: t.Slug, Kind: KindUntested, File: files[i], Line: r.Line, ID: r.ID,
				Message: r.ID + " is implemented but no test file cites it"})
		}
	}
	return out
}

func isTestFile(rel string) bool {
	if testNameRe.MatchString(filepath.Base(rel)) {
		return true
	}
	for _, c := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
		switch c {
		case "test", "tests", "__tests__", "e2e", "testdata":
			return true
		}
	}
	return false
}

// citedIDs collects the requirement IDs mentioned in the test files under
// dir: regular files only (a symlink could lead out of the repo), at most
// MaxSpecSize each, never the specs themselves, .git, node_modules or vendor.
// capped names the limit that stopped the search early ("" when none did).
func citedIDs(dir, specs string) (cited map[string]bool, capped string) {
	out := map[string]bool{}
	seen, read := 0, 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if seen++; seen > maxWalk {
			capped = fmt.Sprintf("%d entries", maxWalk)
			return filepath.SkipAll
		}
		if read >= maxTestFiles {
			capped = fmt.Sprintf("%d test files", maxTestFiles)
			return filepath.SkipAll
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			if p == filepath.Clean(specs) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || !d.Type().IsRegular() || !isTestFile(rel) {
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > MaxSpecSize {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		read++
		for _, id := range anyIDRe.FindAllString(string(b), -1) {
			out[id] = true
		}
		return nil
	})
	return out, capped
}

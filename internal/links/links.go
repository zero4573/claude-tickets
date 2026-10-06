// Package links checks and preserves Obsidian wikilinks ([[x]], ![[x]],
// [[x#heading]], [[x|alias]]) when notes move.
//
// Resolution follows Obsidian: a link target without "/" matches by file
// name (case-insensitive, ".md" implied for notes); with "/" it matches
// the end of the vault-relative path. Links inside code blocks and inline
// code are ignored. .obsidian/, .trash/ and other dot-folders are skipped.
package links

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/zero4573/claude-tickets/internal/fsx"
)

var (
	linkRe       = regexp.MustCompile(`(!?)\[\[([^\[\]|#^]*)([#^][^\[\]|]*)?(\|[^\[\]]*)?\]\]`)
	fenceRe      = regexp.MustCompile("^\\s*(```|~~~)")
	inlineCodeRe = regexp.MustCompile("`[^`\n]*`")
)

// Files is every file in the vault outside dot-folders (and not dot-files).
func Files(vault string) []string {
	var out []string
	_ = filepath.WalkDir(vault, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p != vault && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

type Index struct {
	vault  string
	byName map[string][]string
	rels   [][2]string // lower-cased vault-relative path, path
}

func keys(p string) []string {
	name := strings.ToLower(filepath.Base(p))
	k := []string{name}
	if strings.HasSuffix(name, ".md") {
		k = append(k, strings.TrimSuffix(name, ".md"))
	}
	return k
}

func NewIndex(vault string) *Index {
	ix := &Index{vault: vault, byName: map[string][]string{}}
	for _, p := range Files(vault) {
		rel, _ := filepath.Rel(vault, p)
		for _, k := range keys(p) {
			ix.byName[k] = append(ix.byName[k], p)
		}
		ix.rels = append(ix.rels, [2]string{strings.ToLower(filepath.ToSlash(rel)), p})
	}
	return ix
}

// Resolve is the files a link target names (none for [[#heading]], a link
// within the same note).
func (ix *Index) Resolve(target string) []string {
	t := strings.ToLower(strings.TrimSpace(target))
	if t == "" {
		return nil
	}
	if !strings.Contains(t, "/") {
		return ix.byName[t]
	}
	t = strings.TrimLeft(t, "/")
	var out []string
	for _, r := range ix.rels {
		for _, c := range []string{t, t + ".md"} {
			if r[0] == c || strings.HasSuffix(r[0], "/"+c) {
				out = append(out, r[1])
				break
			}
		}
	}
	return out
}

// lines splits text into lines, each keeping its line ending.
func lines(text string) []string {
	var out []string
	for text != "" {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			out = append(out, text)
			break
		}
		out = append(out, text[:i+1])
		text = text[i+1:]
	}
	return out
}

// Place is where a link is: its note and line, and for a line of the
// frontmatter the field it belongs to; SourceBlock is a line between
// <!-- source:start --> and <!-- source:end --> (a synced ticket's part
// that the sync rewrites).
type Place struct {
	File        string
	Line        int
	Frontmatter bool
	Field       string
	SourceBlock bool
}

const (
	sourceStart = "<!-- source:start -->"
	sourceEnd   = "<!-- source:end -->"
)

var fieldRe = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_-]*):`)

// eachLine calls fn for every line of text (with its line ending), saying
// whether it's code (a fence, or inside a fenced block) and where it is.
func eachLine(file, text string, fn func(l string, code bool, p Place)) {
	fence, front, source := false, false, false
	field := ""
	for i, l := range lines(text) {
		bare := strings.TrimRight(l, "\r\n")
		p := Place{File: file, Line: i + 1}
		switch {
		case i == 0 && strings.TrimPrefix(bare, "\ufeff") == "---":
			front = true
			p.Frontmatter = true
		case front && bare == "---":
			front = false
			p.Frontmatter = true
		case front:
			// An indented line or a list item belongs to the key above it
			if m := fieldRe.FindStringSubmatch(bare); m != nil {
				field = m[1]
			}
			p.Frontmatter, p.Field = true, field
		case fenceRe.MatchString(bare):
			fence = !fence
			fn(l, true, p)
			continue
		case fence:
			fn(l, true, p)
			continue
		default:
			switch strings.TrimSpace(bare) {
			case sourceStart:
				source = true
			case sourceEnd:
				source = false
			}
			p.SourceBlock = source
		}
		fn(l, false, p)
	}
}

// link is one wikilink: where it is, and the regexp's submatches.
type link struct {
	Place
	parts []string
}

// target is a link's target as Obsidian reads it: trimmed, without the
// backslash of a pipe escaped in a table ([[x\|alias]]).
func target(m []string) string {
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m[2]), `\`))
}

// linksIn is the wikilinks of a note's text outside code.
func linksIn(file, text string) []link {
	var out []link
	eachLine(file, text, func(l string, code bool, p Place) {
		if code {
			return
		}
		l = strings.TrimRight(l, "\r\n")
		scrubbed := inlineCodeRe.ReplaceAllStringFunc(l, func(s string) string { return strings.Repeat(" ", len(s)) })
		for _, m := range linkRe.FindAllStringSubmatch(scrubbed, -1) {
			out = append(out, link{p, m})
		}
	})
	return out
}

// at is a link being rewritten: where it is, its line (without the line
// ending) and its span in the line.
type at struct {
	Place
	line       string
	start, end int
}

// rewriteLinks rewrites the wikilinks of a note's text outside code with
// repl (given the regexp's submatches and where the link is, it returns
// the replacement).
func rewriteLinks(file, text string, repl func(m []string, a at) string) string {
	var b strings.Builder
	eachLine(file, text, func(l string, code bool, p Place) {
		if code {
			b.WriteString(l)
			return
		}
		bare := strings.TrimRight(l, "\r\n")
		sub := func(from, to int) {
			seg, prev := l[from:to], 0
			for _, loc := range linkRe.FindAllStringSubmatchIndex(seg, -1) {
				m := make([]string, len(loc)/2)
				for i := range m {
					if loc[2*i] >= 0 {
						m[i] = seg[loc[2*i]:loc[2*i+1]]
					}
				}
				b.WriteString(seg[prev:loc[0]])
				b.WriteString(repl(m, at{p, bare, from + loc[0], from + loc[1]}))
				prev = loc[1]
			}
			b.WriteString(seg[prev:])
		}
		// Inline code spans stay as they are
		last := 0
		for _, loc := range inlineCodeRe.FindAllStringIndex(l, -1) {
			sub(last, loc[0])
			b.WriteString(l[loc[0]:loc[1]])
			last = loc[1]
		}
		sub(last, len(l))
	})
	return b.String()
}

// replaceOutsideCode rewrites the wikilinks of text outside code with repl
// (given the regexp's submatches, it returns the replacement).
func replaceOutsideCode(text string, repl func(m []string) string) string {
	return rewriteLinks("", text, func(m []string, _ at) string { return repl(m) })
}

// notes is every note in the vault outside templates/.
func notes(vault string) []string {
	var out []string
	for _, p := range Files(vault) {
		rel, _ := filepath.Rel(vault, p)
		if strings.HasSuffix(p, ".md") && strings.Split(filepath.ToSlash(rel), "/")[0] != "templates" {
			out = append(out, p)
		}
	}
	return out
}

// Problem is a link that resolves to no file (Found empty) or to several,
// or a file to check that doesn't exist (Missing).
type Problem struct {
	Place
	Target  string
	Found   []string
	Missing bool
}

// Problems is the broken links in files (default: every note outside
// templates/).
func Problems(vault string, files []string) []Problem {
	ix := NewIndex(vault)
	if len(files) == 0 {
		files = notes(vault)
	}
	var out []Problem
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			out = append(out, Problem{Place: Place{File: f}, Missing: true})
			continue
		}
		for _, l := range linksIn(f, string(data)) {
			t := target(l.parts)
			if t == "" {
				continue
			}
			if found := ix.Resolve(t); len(found) != 1 {
				out = append(out, Problem{Place: l.Place, Target: t, Found: found})
			}
		}
	}
	return out
}

// CheckOpts tunes CheckWith.
type CheckOpts struct {
	// Exempt marks unresolved links that aren't counted as problems: they
	// get one summary line instead, ExemptNote (with %d for their number).
	Exempt     func(p Place, target string) bool
	ExemptNote string
}

// Check reports the wikilinks in files (default: every note outside
// templates/) that resolve to no file, or to several (ambiguous by bare
// name), one per line on w. It returns the number of problems.
func Check(vault string, files []string, w io.Writer) int {
	return CheckWith(vault, files, w, CheckOpts{})
}

// CheckWith is Check, leaving out the unresolved links o.Exempt marks.
func CheckWith(vault string, files []string, w io.Writer, o CheckOpts) int {
	if len(files) == 0 {
		files = notes(vault)
	}
	problems, exempt := 0, 0
	for _, p := range Problems(vault, files) {
		rel := p.File
		if r, err := filepath.Rel(vault, p.File); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
		switch {
		case p.Missing:
			fmt.Fprintf(w, "%s: file not found\n", p.File)
		case len(p.Found) == 0 && o.Exempt != nil && o.Exempt(p.Place, p.Target):
			exempt++
			continue
		case len(p.Found) == 0:
			fmt.Fprintf(w, "%s:%d: unresolved link [[%s]]\n", rel, p.Line, p.Target)
		default:
			var where []string
			for _, f := range p.Found {
				r, _ := filepath.Rel(vault, f)
				where = append(where, r)
			}
			fmt.Fprintf(w, "%s:%d: ambiguous link [[%s]] -> %s\n", rel, p.Line, p.Target, strings.Join(where, ", "))
		}
		problems++
	}
	if exempt > 0 && o.ExemptNote != "" {
		fmt.Fprintf(w, o.ExemptNote+"\n", exempt)
	}
	if problems == 0 {
		fmt.Fprintf(w, "ct vault links: %d file(s) checked, all links resolve\n", len(files))
	}
	return problems
}

// resolvePath is an absolute path with symlinks resolved as far as it
// exists.
func resolvePath(p string) string {
	p, _ = filepath.Abs(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if r, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		return filepath.Join(r, filepath.Base(p))
	}
	return p
}

func inside(p, dir string) bool { return fsx.Within(p, dir) }

// Move moves a note (or attachment) inside the vault, keeping every link
// to it working: it refuses when another file already has the
// destination's name (bare-name links would become ambiguous), then
// rewrites path-qualified links ([[old/path/note]]) across the vault to
// the new path (and bare-name links too on a rename). dst may be a folder.
// It returns the new vault-relative path and how many links it rewrote.
func Move(vault, src, dst string) (string, int, error) {
	vault = resolvePath(vault)
	if !filepath.IsAbs(src) {
		src = filepath.Join(vault, src)
	}
	if !filepath.IsAbs(dst) {
		dst = filepath.Join(vault, dst)
	}
	src = resolvePath(src)
	if st, err := os.Stat(src); err != nil || !st.Mode().IsRegular() {
		return "", 0, fmt.Errorf("no such file: %s", src)
	}
	if st, err := os.Stat(dst); err == nil && st.IsDir() {
		dst = filepath.Join(dst, filepath.Base(src))
	}
	dst = resolvePath(dst)
	if !inside(src, vault) || !inside(dst, vault) {
		return "", 0, errors.New("source and destination must both be inside the vault")
	}
	if _, err := os.Lstat(dst); err == nil {
		r, _ := filepath.Rel(vault, dst)
		return "", 0, fmt.Errorf("destination exists: %s", r)
	}
	ix := NewIndex(vault)
	clash := map[string]bool{}
	for _, k := range keys(dst) {
		for _, p := range ix.byName[k] {
			if resolvePath(p) != src {
				r, _ := filepath.Rel(vault, p)
				clash[r] = true
			}
		}
	}
	if len(clash) > 0 {
		var where []string
		for r := range clash {
			where = append(where, r)
		}
		sort.Strings(where)
		return "", 0, fmt.Errorf("name '%s' is already used by %s; links to it would become ambiguous", filepath.Base(dst), strings.Join(where, ", "))
	}

	oldRel, _ := filepath.Rel(vault, src)
	newRel, _ := filepath.Rel(vault, dst)
	oldRel, newRel = filepath.ToSlash(oldRel), filepath.ToSlash(newRel)
	forms := pathForms(oldRel)
	newTarget := linkPath(newRel)
	// A rename also breaks bare-name links ([[old]]): they're rewritten when
	// the old name meant only this file (else they named another one)
	bareOld, bareNew := map[string]bool{}, ""
	if !strings.EqualFold(filepath.Base(src), filepath.Base(dst)) {
		unique := true
		for _, k := range keys(src) {
			for _, p := range ix.byName[k] {
				unique = unique && resolvePath(p) == src
			}
		}
		if unique {
			for _, k := range keys(src) {
				bareOld[k] = true
			}
			bareNew = path.Base(newTarget)
		}
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", 0, err
	}
	if err := moveFile(src, dst); err != nil {
		return "", 0, err
	}

	rewritten := 0
	for _, f := range Files(vault) {
		if !strings.HasSuffix(f, ".md") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		text := string(data)
		out := replaceOutsideCode(text, func(m []string) string {
			t := strings.ToLower(strings.TrimLeft(strings.TrimSpace(m[2]), "/"))
			if !strings.Contains(t, "/") {
				if bareOld[t] {
					rewritten++
					return m[1] + "[[" + bareNew + m[3] + m[4] + "]]"
				}
			} else if namesPath(t, forms) {
				rewritten++
				return m[1] + "[[" + newTarget + m[3] + m[4] + "]]"
			}
			return m[0]
		})
		if out != text {
			st, _ := os.Stat(f)
			if err := os.WriteFile(f, []byte(out), st.Mode().Perm()); err != nil {
				return newRel, rewritten, err
			}
		}
	}
	return newRel, rewritten, nil
}

// pathForms is the lower-cased forms a path-qualified link to the file at
// vault-relative rel ("/"-separated) can take: with and, for a note,
// without .md.
func pathForms(rel string) []string {
	forms := []string{strings.ToLower(rel)}
	if strings.HasSuffix(rel, ".md") {
		forms = append(forms, strings.ToLower(strings.TrimSuffix(rel, ".md")))
	}
	return forms
}

// namesPath reports whether path-qualified target t (lower-cased, no
// leading "/") names the file with these forms. It matches the end of the
// path, so [[sequences/flow]] points at projects/x/sequences/flow.md too.
func namesPath(t string, forms []string) bool {
	for _, form := range forms {
		if form == t || strings.HasSuffix(form, "/"+t) {
			return true
		}
	}
	return false
}

// linkPath is the target of a path-qualified link to vault-relative rel
// (a note's without .md).
func linkPath(rel string) string { return strings.TrimSuffix(rel, ".md") }

// moveFile renames, or copies and removes across filesystems.
func moveFile(src, dst string) error {
	if os.Rename(src, dst) == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, st.Mode().Perm()); err != nil {
		return err
	}
	return os.Remove(src)
}

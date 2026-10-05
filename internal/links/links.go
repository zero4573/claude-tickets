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

// link is one wikilink: its line, and the regexp's submatches.
type link struct {
	line  int
	parts []string
}

// linksIn is the wikilinks of text outside code.
func linksIn(text string) []link {
	var out []link
	fence := false
	for i, l := range lines(text) {
		l = strings.TrimRight(l, "\r\n")
		if fenceRe.MatchString(l) {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		scrubbed := inlineCodeRe.ReplaceAllStringFunc(l, func(s string) string { return strings.Repeat(" ", len(s)) })
		for _, m := range linkRe.FindAllStringSubmatch(scrubbed, -1) {
			out = append(out, link{i + 1, m})
		}
	}
	return out
}

// replaceOutsideCode rewrites the wikilinks of text outside code with repl
// (given the regexp's submatches, it returns the replacement).
func replaceOutsideCode(text string, repl func(m []string) string) string {
	var b strings.Builder
	fence := false
	sub := func(s string) string {
		return linkRe.ReplaceAllStringFunc(s, func(x string) string { return repl(linkRe.FindStringSubmatch(x)) })
	}
	for _, l := range lines(text) {
		if fenceRe.MatchString(strings.TrimRight(l, "\r\n")) {
			fence = !fence
			b.WriteString(l)
			continue
		}
		if fence {
			b.WriteString(l)
			continue
		}
		// Inline code spans stay as they are
		last := 0
		for _, loc := range inlineCodeRe.FindAllStringIndex(l, -1) {
			b.WriteString(sub(l[last:loc[0]]))
			b.WriteString(l[loc[0]:loc[1]])
			last = loc[1]
		}
		b.WriteString(sub(l[last:]))
	}
	return b.String()
}

// Check reports the wikilinks in files (default: every note outside
// templates/) that resolve to no file, or to several (ambiguous by bare
// name), one per line on w. It returns the number of problems.
func Check(vault string, files []string, w io.Writer) int {
	ix := NewIndex(vault)
	if len(files) == 0 {
		for _, p := range Files(vault) {
			rel, _ := filepath.Rel(vault, p)
			if strings.HasSuffix(p, ".md") && strings.Split(filepath.ToSlash(rel), "/")[0] != "templates" {
				files = append(files, p)
			}
		}
	}
	problems := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(w, "%s: file not found\n", f)
			problems++
			continue
		}
		rel := f
		if r, err := filepath.Rel(vault, f); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
		for _, l := range linksIn(string(data)) {
			target := l.parts[2]
			if strings.TrimSpace(target) == "" {
				continue
			}
			switch found := ix.Resolve(target); {
			case len(found) == 0:
				fmt.Fprintf(w, "%s:%d: unresolved link [[%s]]\n", rel, l.line, target)
				problems++
			case len(found) > 1:
				var where []string
				for _, p := range found {
					r, _ := filepath.Rel(vault, p)
					where = append(where, r)
				}
				fmt.Fprintf(w, "%s:%d: ambiguous link [[%s]] -> %s\n", rel, l.line, target, strings.Join(where, ", "))
				problems++
			}
		}
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
	forms := []string{strings.ToLower(oldRel)}
	if strings.HasSuffix(src, ".md") {
		forms = append(forms, strings.ToLower(strings.TrimSuffix(oldRel, ".md")))
	}
	newTarget := newRel
	if strings.HasSuffix(dst, ".md") {
		newTarget = strings.TrimSuffix(newRel, ".md")
	}
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
			// A path-qualified target matches the end of the path, so
			// [[sequences/flow]] points at projects/x/sequences/flow.md too
			t := strings.ToLower(strings.TrimLeft(strings.TrimSpace(m[2]), "/"))
			if !strings.Contains(t, "/") {
				if bareOld[t] {
					rewritten++
					return m[1] + "[[" + bareNew + m[3] + m[4] + "]]"
				}
			} else {
				for _, form := range forms {
					if form == t || strings.HasSuffix(form, "/"+t) {
						rewritten++
						return m[1] + "[[" + newTarget + m[3] + m[4] + "]]"
					}
				}
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

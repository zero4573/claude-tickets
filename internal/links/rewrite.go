package links

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/zero4573/claude-tickets/internal/fsx"
)

// RewriteOpts tunes RewriteToURL.
type RewriteOpts struct {
	DryRun bool
	// Skip leaves the links at a place as they are (true)
	Skip func(Place) bool
	// NotRewritten hears of a frontmatter link in a form that can't hold a
	// markdown link (it's left as it is)
	NotRewritten func(Place)
	// Changed hears of each note rewritten (or that would be, on a dry run)
	Changed func(file string)
}

// wholeValue is what comes before a frontmatter link that is a field's
// whole value: "key: " or a block list's "- ".
var wholeValue = regexp.MustCompile(`^(\s*-\s*|[A-Za-z0-9_][A-Za-z0-9_-]*:\s*)$`)

// RewriteToURL turns every link that resolves only to file (a note in
// the vault) into a markdown link to url, across the vault's notes outside
// templates/ and skipDirs, outside code: [[ID]], [[ID|alias]],
// [[ID#heading]], ![[ID]] and path-qualified forms become [ID](url) or
// [alias](url) (a heading or block can't be linked on the remote page; an
// embed can't embed it). In frontmatter, a link directly inside quotes or
// a field's whole value becomes "[ID](url)"; other forms are left (o.
// NotRewritten). It returns the links rewritten and the notes changed.
func RewriteToURL(vault, file, id, url string, skipDirs []string, o RewriteOpts) (n, changed int, err error) {
	vault, file = resolvePath(vault), resolvePath(file)
	skipDirs = resolveAll(skipDirs)
	if strings.ContainsAny(url, " ()<") {
		url = "<" + url + ">"
	}
	ix := NewIndex(vault)
	only := func(t string) bool {
		found := ix.Resolve(t)
		for _, f := range found {
			if filepath.Clean(f) != file {
				return false
			}
		}
		return len(found) > 0
	}
	for _, f := range notes(vault) {
		if skipped(f, skipDirs) {
			continue
		}
		data, rerr := os.ReadFile(f)
		if rerr != nil {
			continue
		}
		text, links := string(data), 0
		out := rewriteLinks(f, text, func(m []string, a at) string {
			t := target(m)
			if t == "" || !only(t) || o.Skip != nil && o.Skip(a.Place) {
				return m[0]
			}
			label := id
			if alias := strings.TrimSpace(strings.TrimPrefix(m[4], "|")); alias != "" {
				label = alias
			}
			md := "[" + label + "](" + url + ")"
			if !a.Frontmatter {
				links++
				return md
			}
			before, after := a.line[:a.start], a.line[a.end:]
			for _, q := range []string{`"`, `'`} {
				if strings.HasSuffix(before, q) && strings.HasPrefix(after, q) && !strings.ContainsAny(md, q+`\`) {
					links++
					return md
				}
			}
			if wholeValue.MatchString(before) && strings.TrimSpace(after) == "" && !strings.ContainsAny(md, `"\`) {
				links++
				return `"` + md + `"`
			}
			if o.NotRewritten != nil {
				o.NotRewritten(a.Place)
			}
			return m[0]
		})
		if out == text {
			continue
		}
		n += links
		changed++
		if o.Changed != nil {
			o.Changed(f)
		}
		if !o.DryRun {
			if err := fsx.WriteFileAtomic(f, []byte(out), 0o644); err != nil {
				return n, changed, err
			}
		}
	}
	return n, changed, nil
}

func resolveAll(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = resolvePath(p)
	}
	return out
}

func skipped(f string, dirs []string) bool {
	for _, d := range dirs {
		if fsx.Within(f, d) {
			return true
		}
	}
	return false
}

// Referenced is the files under dir that a note outside dir (and outside
// exclude) links or embeds, where every file the link resolves to is
// under dir; closed over the links between files of dir, so a kept note
// keeps what it embeds. The folder's own note (dir/<name of dir>.md, a
// ticket's note) is never in it: links to it are rewritten instead.
func Referenced(vault, dir string, exclude []string) []string {
	vault, dir = resolvePath(vault), resolvePath(dir)
	exclude = resolveAll(exclude)
	own := filepath.Join(dir, filepath.Base(dir)+".md")
	ix := NewIndex(vault)
	kept := map[string]bool{}
	var queue []string
	scan := func(f string) {
		data, err := os.ReadFile(f)
		if err != nil {
			return
		}
		for _, l := range linksIn(f, string(data)) {
			found := ix.Resolve(target(l.parts))
			inside := len(found) > 0
			for _, p := range found {
				inside = inside && fsx.Inside(p, dir)
			}
			if !inside {
				continue
			}
			for _, p := range found {
				if p != own && !kept[p] {
					kept[p] = true
					if strings.HasSuffix(p, ".md") {
						queue = append(queue, p)
					}
				}
			}
		}
	}
	for _, f := range notes(vault) {
		if !fsx.Within(f, dir) && !skipped(f, exclude) {
			scan(f)
		}
	}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		scan(f)
	}
	out := make([]string, 0, len(kept))
	for p := range kept {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

package cite

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/zero4573/tix/internal/gitx"
)

// Failure is one citation that doesn't hold.
type Failure struct {
	Line     int
	Citation string
	Reason   string
}

// Checker checks a note's citations. Pins (Key → commit) wins over the
// note's own; Resolve finds the clone of a repo Key (any clone that has
// the pinned commits: main clone or worktree).
type Checker struct {
	Pins    map[string]string
	Resolve func(key string) (dir string, err error)
	Symbols bool // also check a caption's symbols appear in its range

	trees map[string]map[string]bool // dir@rev → paths
	blobs map[string][]string        // dir@rev:path → lines
	dirs  map[string]string          // key → dir
	errs  map[string]string          // key or dir@rev → why it can't be read
}

// elisionRe is a line a chunk shortens the code with: an ellipsis alone,
// or as the start of a comment (`// …`, `# ...`, `-- …`, `/* … */`).
var elisionRe = regexp.MustCompile(`^\s*(?:(?://|#|--|;|%|/\*|<!--|\(\*)\s*(?:…|\.\.\.).*|(?:…|\.\.\.))\s*$`)

// Check returns the failures of n's citations, in note order.
func (c *Checker) Check(n Note) []Failure {
	if c.trees == nil {
		c.trees, c.blobs = map[string]map[string]bool{}, map[string][]string{}
		c.dirs, c.errs = map[string]string{}, map[string]string{}
	}
	pins := map[string]string{}
	for k, v := range n.Pins {
		pins[k] = v
	}
	for k, v := range c.Pins {
		pins[Key(k)] = v
	}
	var out []Failure
	for _, cit := range n.Citations {
		if reason := c.check(cit, pins); reason != "" {
			out = append(out, Failure{Line: cit.Line, Citation: cit.Text, Reason: reason})
		}
	}
	return out
}

func (c *Checker) check(cit Citation, pins map[string]string) string {
	if cit.Start < 1 || cit.End < cit.Start {
		return fmt.Sprintf("bad range %d-%d", cit.Start, cit.End)
	}
	key, file, reason := c.locate(cit, pins)
	if reason != "" {
		return reason
	}
	dir, rev := c.dirs[key], pins[key]
	lines, err := c.blob(dir, rev, file)
	if err != "" {
		return err
	}
	at := fmt.Sprintf("%s at %s", file, short(rev))
	if cit.End > len(lines) {
		return fmt.Sprintf("lines %d-%d are outside %s (%d lines)", cit.Start, cit.End, at, len(lines))
	}
	span := lines[cit.Start-1 : cit.End]
	if cit.Chunk != nil {
		if r := matchChunk(cit.Chunk, span, cit.Start); r != "" {
			return r + " (" + at + ")"
		}
	}
	if c.Symbols {
		text := strings.Join(span, "\n")
		for _, s := range cit.Symbols {
			if name := symbolName(s); name != "" && !strings.Contains(text, name) {
				return fmt.Sprintf("symbol %s is not in lines %d-%d of %s", name, cit.Start, cit.End, at)
			}
		}
	}
	return ""
}

// locate finds the repo (its Key) and the file a citation means: the repo
// it names, else the one pinned repo that has the file; a path that is
// not in the tree as given may be the end of exactly one path there (a
// note's `api.ex:63`).
func (c *Checker) locate(cit Citation, pins map[string]string) (key, file, reason string) {
	var keys []string
	if cit.Repo != "" {
		key = Key(cit.Repo)
		if _, ok := pins[key]; !ok {
			return "", "", fmt.Sprintf("no pinned commit for %s (state `%s `<branch>` @ `<sha>`` in the note, or pass --pin %s=<rev>)", cit.Repo, cit.Repo, cit.Repo)
		}
		keys = []string{key}
	} else {
		for k := range pins {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			return "", "", "no pinned commit in the note (state `<repo> `<branch>` @ `<sha>``, or pass --pin <repo>=<rev>)"
		}
	}
	type hit struct{ key, file string }
	var hits []hit
	var why []string
	for _, k := range keys {
		tree, r := c.tree(k, pins[k])
		if r != "" {
			why = append(why, r)
			continue
		}
		for _, f := range findPath(tree, cit.Path) {
			hits = append(hits, hit{k, f})
		}
	}
	switch {
	case len(hits) == 1:
		return hits[0].key, hits[0].file, ""
	case len(hits) > 1:
		var names []string
		for _, h := range hits {
			if len(keys) > 1 {
				names = append(names, h.key+":"+h.file)
			} else {
				names = append(names, h.file)
			}
		}
		return "", "", "ambiguous: " + strings.Join(names, ", ")
	case len(why) > 0:
		return "", "", strings.Join(why, "; ")
	}
	where := make([]string, len(keys))
	for i, k := range keys {
		where[i] = k + " at " + short(pins[k])
	}
	return "", "", fmt.Sprintf("%s is not in %s", cit.Path, strings.Join(where, ", "))
}

// findPath is the path itself when the tree has it, else every path that
// ends with /<p>.
func findPath(tree map[string]bool, p string) []string {
	p = strings.TrimPrefix(path.Clean(strings.TrimPrefix(p, "/")), "./")
	if tree[p] {
		return []string{p}
	}
	var out []string
	for f := range tree {
		if strings.HasSuffix(f, "/"+p) {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// tree lists the files of the repo at rev, once.
func (c *Checker) tree(key, rev string) (map[string]bool, string) {
	dir, ok := c.dirs[key]
	if !ok {
		if r, seen := c.errs[key]; seen {
			return nil, r
		}
		d, err := c.Resolve(key)
		if err != nil {
			c.errs[key] = err.Error()
			return nil, c.errs[key]
		}
		c.dirs[key], dir = d, d
	}
	id := dir + "@" + rev
	if t, ok := c.trees[id]; ok {
		return t, ""
	}
	if r, seen := c.errs[id]; seen {
		return nil, r
	}
	if err := gitx.Check(dir); err != nil {
		c.errs[id] = fmt.Sprintf("can't read %s: %v", dir, err)
		return nil, c.errs[id]
	}
	if strings.HasPrefix(rev, "-") || !gitx.Ok(dir, "rev-parse", "-q", "--verify", rev+"^{commit}") {
		c.errs[id] = fmt.Sprintf("%s is not a commit in %s", rev, dir)
		return nil, c.errs[id]
	}
	out, err := gitx.OutIn(dir, nil, "ls-tree", "-r", "-z", "--name-only", rev+"^{commit}")
	if err != nil {
		c.errs[id] = fmt.Sprintf("can't list %s at %s: %v", dir, short(rev), err)
		return nil, c.errs[id]
	}
	t := map[string]bool{}
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			t[f] = true
		}
	}
	c.trees[id] = t
	return t, ""
}

// blob is the file's lines at rev (CR LF read as LF).
func (c *Checker) blob(dir, rev, file string) ([]string, string) {
	id := dir + "@" + rev + ":" + file
	if l, ok := c.blobs[id]; ok {
		return l, ""
	}
	// line by line, so blank lines at the end of the file count
	var lines []string
	err := gitx.Lines(dir, func(l string) {
		lines = append(lines, strings.TrimSuffix(l, "\r"))
	}, "cat-file", "blob", rev+"^{commit}:"+file)
	if err != nil {
		return nil, fmt.Sprintf("can't read %s at %s: %v", file, short(rev), err)
	}
	c.blobs[id] = lines
	return lines, ""
}

// matchChunk says why the chunk is not the file's text at span (which
// starts at line start), "" when it is: every chunk line is the next file
// line (leading and trailing whitespace ignored: a chunk from inside a
// block is often dedented), except that an elision line skips ahead to
// wherever the line after it is found. The chunk ends at the span's end
// unless it ends with an elision.
func matchChunk(chunk, span []string, start int) string {
	pos, skip := 0, false
	same := func(a, b string) bool { return strings.TrimSpace(a) == strings.TrimSpace(b) }
	for i, l := range chunk {
		if elisionRe.MatchString(l) {
			skip = true
			continue
		}
		if skip {
			found := -1
			for j := pos; j < len(span); j++ {
				if same(span[j], l) {
					found = j
					break
				}
			}
			if found < 0 {
				return fmt.Sprintf("chunk line %d (after an elision) is not in lines %d-%d: %q", i+1, start+pos, start+len(span)-1, clip(l))
			}
			pos, skip = found, false
		}
		if pos >= len(span) {
			return fmt.Sprintf("chunk line %d is past line %d, the caption's end", i+1, start+len(span)-1)
		}
		if !same(span[pos], l) {
			return fmt.Sprintf("chunk line %d differs from line %d: has %q, the file %q", i+1, start+pos, clip(l), clip(span[pos]))
		}
		pos++
	}
	if !skip && pos < len(span) {
		return fmt.Sprintf("chunk ends at line %d, the caption says %d", start+pos-1, start+len(span)-1)
	}
	return ""
}

// symbolName is the name a caption's symbol is looked up by: its last
// part (`Pkg.Func`, `Mod::fun`, `Class#method`), without a call's
// parentheses; "" when nothing is left.
func symbolName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "()")
	if k := strings.IndexByte(s, '('); k > 0 {
		s = s[:k]
	}
	if k := strings.LastIndexAny(s, ".#/:"); k >= 0 {
		s = s[k+1:]
	}
	return strings.TrimSpace(s)
}

func short(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}

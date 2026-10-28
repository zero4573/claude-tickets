package diffcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/zero4573/claude-tickets/internal/gitx"
)

// MessagePath is the path reported for a hit in a commit message.
const MessagePath = ":commit-message"

// IDHit is one vault-only ID found in one (commit, path).
type IDHit struct {
	ID     string
	Commit string // short sha
	Path   string // MessagePath for a commit message
}

// Only entry lines count, never a mention in prose: a relay entry starts
// with `R-nn · `, a backlog item is `- [.] B-nn · `, a review finding is
// `- W<N>-<repo>-<M|S|C>nn · ` and its disposition `- [.] W… · `.
var (
	relayIDRe   = regexp.MustCompile(`(?m)^(R-[0-9]+) · `)
	backlogIDRe = regexp.MustCompile(`(?m)^- \[.\] (B-[0-9]+) · `)
	findingIDRe = regexp.MustCompile(`(?m)^- (?:\[.\] )?(W[0-9]+-[A-Za-z0-9-]+-[MSC][0-9]+) · `)
)

// TicketIDs is the ticket's vault-only ID set, read from its folder: the
// ticket ID, the R-nn of relay.md's entries, the B-nn of backlog.md's
// items and the finding IDs of the findings and dispositions in
// reviews/*.md. A missing file adds nothing. Sorted, unique.
func TicketIDs(folder, id string) []string {
	set := map[string]bool{id: true}
	add := func(file string, re *regexp.Regexp) {
		b, err := os.ReadFile(file)
		if err != nil {
			return
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			set[m[1]] = true
		}
	}
	add(filepath.Join(folder, "relay.md"), relayIDRe)
	add(filepath.Join(folder, "backlog.md"), backlogIDRe)
	reviews, _ := filepath.Glob(filepath.Join(folder, "reviews", "*.md"))
	for _, f := range reviews {
		add(f, findingIDRe)
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// idMatcher finds whole-word occurrences of any of a set of IDs with one
// alternation regex. IDs start and end with a word character, so `\b`
// marks a whole word (as `[^A-Za-z0-9_]` or the line's edge would).
type idMatcher struct{ re *regexp.Regexp }

func newIDMatcher(ids []string) idMatcher {
	if len(ids) == 0 {
		return idMatcher{}
	}
	alts := make([]string, len(ids))
	for i, id := range ids {
		alts[i] = regexp.QuoteMeta(id)
	}
	// longest first, so a prefix never shadows a longer ID
	sort.Slice(alts, func(i, j int) bool { return len(alts[i]) > len(alts[j]) })
	return idMatcher{re: regexp.MustCompile(`\b(?:` + strings.Join(alts, "|") + `)\b`)}
}

// count adds each occurrence of an ID in s to into.
func (m idMatcher) count(s string, into map[string]int) {
	if m.re == nil {
		return
	}
	for _, id := range m.re.FindAllString(s, -1) {
		into[id]++
	}
}

// Revs lists the non-merge commits of revRange (full shas) in rev-list
// order.
func Revs(dir, revRange string) ([]string, error) {
	if strings.HasPrefix(revRange, "-") {
		return nil, fmt.Errorf("bad range '%s' (it must not start with '-')", revRange)
	}
	out, err := gitx.OutIn(dir, nil, "rev-list", "--no-merges", revRange, "--")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// CheckIDs lists the vault-only ids that the non-merge commits of
// revRange introduce, in rev-list order, then path (the message first),
// then ID. A commit message is flagged for any whole-word occurrence; a
// file only for an ID that occurs more often in the commit's added lines
// of it than in its removed ones, so a line kept from the base passes.
// Binary files have no lines. Read-only.
func CheckIDs(dir, revRange string, ids []string) ([]IDHit, error) {
	revs, err := Revs(dir, revRange)
	if err != nil {
		return nil, err
	}
	m := newIDMatcher(ids)
	var hits []IDHit
	for _, sha := range revs {
		h, err := commitIDs(dir, sha, m)
		if err != nil {
			return nil, err
		}
		hits = append(hits, h...)
	}
	return hits, nil
}

func commitIDs(dir, sha string, m idMatcher) ([]IDHit, error) {
	msg, err := gitx.OutIn(dir, nil, "log", "-1", "--format=%B", sha)
	if err != nil {
		return nil, err
	}
	var hits []IDHit
	inMsg := map[string]int{}
	m.count(msg, inMsg)
	for _, id := range sortedKeys(inMsg) {
		hits = append(hits, IDHit{ID: id, Commit: ShortSHA(sha), Path: MessagePath})
	}
	diffArgs := []string{"diff-tree", "-r", "--root", "--no-renames", "--no-commit-id", "--no-ext-diff", "--no-textconv"}
	// the paths, raw, in the order of the patch's `diff --git` blocks
	// (--name-status: a type change, T, is two blocks, a delete and an add)
	names, err := gitx.OutIn(dir, nil, append(diffArgs, "-z", "--name-status", sha)...)
	if err != nil {
		return nil, err
	}
	var paths []string
	if names != "" {
		f := strings.Split(strings.TrimRight(names, "\x00"), "\x00")
		for i := 0; i+1 < len(f); i += 2 {
			paths = append(paths, f[i+1])
			if f[i] == "T" {
				paths = append(paths, f[i+1])
			}
		}
	}
	type counts struct{ plus, minus map[string]int }
	per := map[string]*counts{}
	block, inHunk := -1, false
	var cur *counts
	err = gitx.Lines(dir, func(l string) {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			block++
			inHunk, cur = false, nil
			if block < len(paths) {
				p := paths[block]
				if per[p] == nil {
					per[p] = &counts{map[string]int{}, map[string]int{}}
				}
				cur = per[p]
			}
		case strings.HasPrefix(l, "@@"):
			inHunk = true
		case !inHunk || cur == nil:
		case strings.HasPrefix(l, "+"):
			m.count(l[1:], cur.plus)
		case strings.HasPrefix(l, "-"):
			m.count(l[1:], cur.minus)
		}
	}, append(diffArgs, "-p", "--no-color", sha)...)
	if err != nil {
		return nil, err
	}
	if block+1 != len(paths) {
		return nil, fmt.Errorf("diff-tree listed %d paths for %d diffs of %s", len(paths), block+1, ShortSHA(sha))
	}
	// an ID is introduced when the commit as a whole adds more than it
	// removes (a moved line or a renamed file keeps it); it is reported at
	// the paths that add it
	plus, minus := map[string]int{}, map[string]int{}
	var order []string
	for p, c := range per {
		order = append(order, p)
		for id, n := range c.plus {
			plus[id] += n
		}
		for id, n := range c.minus {
			minus[id] += n
		}
	}
	sort.Strings(order)
	for _, p := range order {
		c := per[p]
		for _, id := range sortedKeys(c.plus) {
			if plus[id] > minus[id] && c.plus[id] > c.minus[id] {
				hits = append(hits, IDHit{ID: id, Commit: ShortSHA(sha), Path: p})
			}
		}
	}
	return hits, nil
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

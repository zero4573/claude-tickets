package diffcheck

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/spec"
)

// SpecHit is one requirement ID that is taken twice.
type SpecHit struct {
	ID     string
	Commit string // short sha of the range's end
	Path   string // the spec file at the range's end that defines it
	Reason string // "clash" (also defined at the range's start) or "duplicate" (twice at its end)
}

// splitRange reads revRange as A..B or A...B (an empty side is HEAD) and
// returns the start (A), the merge base of both sides and the end (B). ok is
// false for any other form (a single revision has no start).
func splitRange(dir, revRange string) (start, base, end string, ok bool, err error) {
	var l, r string
	if i := strings.Index(revRange, "..."); i >= 0 {
		l, r = revRange[:i], revRange[i+3:]
	} else if i := strings.Index(revRange, ".."); i >= 0 {
		l, r = revRange[:i], revRange[i+2:]
	} else {
		return "", "", "", false, nil
	}
	if l == "" {
		l = "HEAD"
	}
	if r == "" {
		r = "HEAD"
	}
	rev := func(s string) (string, error) {
		out, err := gitx.Out(dir, "rev-parse", "--verify", "--end-of-options", s+"^{commit}")
		return strings.TrimSpace(out), err
	}
	if start, err = rev(l); err != nil {
		return
	}
	if end, err = rev(r); err != nil {
		return
	}
	mb, err := gitx.Out(dir, "merge-base", start, end)
	if err != nil {
		// unrelated histories: nothing is shared, so everything at the end is new
		return start, "", end, true, nil
	}
	base = strings.TrimSpace(mb)
	return start, base, end, true, nil
}

// specRefs lists the requirement IDs defined by the repo's specs/<capability>/spec.md
// at rev (none for an empty rev or a repo without specs/), in path order.
func specRefs(dir, rev string) ([]spec.IDRef, error) {
	if rev == "" {
		return nil, nil
	}
	out, err := gitx.Out(dir, "ls-tree", "-r", "--name-only", "-z", rev, "--", "specs")
	if err != nil {
		return nil, err
	}
	var refs []spec.IDRef
	for _, p := range strings.Split(out, "\x00") {
		if p == "" || path.Base(p) != "spec.md" || path.Dir(path.Dir(p)) != "specs" {
			continue
		}
		src, err := gitx.Out(dir, "cat-file", "blob", rev+":"+p)
		if err != nil {
			return nil, err
		}
		refs = append(refs, spec.FindIDs(p, []byte(src))...)
	}
	return refs, nil
}

// CheckSpecIDs flags requirement IDs of the repo's specs/ that are taken
// twice: an ID added in revRange (A..B or A...B; defined at B but not at the
// merge base) that is already defined at A, or an ID defined twice at B.
// A repo without specs/ gives nothing. Read-only.
func CheckSpecIDs(dir, revRange string) ([]SpecHit, error) {
	start, base, end, ok, err := splitRange(dir, revRange)
	if err != nil || !ok {
		return nil, err
	}
	endRefs, err := specRefs(dir, end)
	if err != nil || len(endRefs) == 0 {
		return nil, err
	}
	baseRefs, err := specRefs(dir, base)
	if err != nil {
		return nil, err
	}
	startRefs := baseRefs
	if start != base {
		if startRefs, err = specRefs(dir, start); err != nil {
			return nil, err
		}
	}
	inBase := map[string]bool{}
	for _, r := range baseRefs {
		inBase[r.ID] = true
	}
	inStart := map[string]bool{}
	for _, r := range startRefs {
		inStart[r.ID] = true
	}
	short := ShortSHA(end)
	seen := map[string]bool{}
	var hits []SpecHit
	add := func(r spec.IDRef, reason string) {
		k := reason + "\x00" + r.ID + "\x00" + r.File
		if !seen[k] {
			seen[k] = true
			hits = append(hits, SpecHit{ID: r.ID, Commit: short, Path: r.File, Reason: reason})
		}
	}
	for _, r := range endRefs {
		if !inBase[r.ID] && inStart[r.ID] {
			add(r, "clash")
		}
	}
	dups := spec.Duplicates(endRefs)
	for _, id := range spec.SortedIDs(dups) {
		for _, r := range dups[id] {
			add(r, "duplicate")
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].ID < hits[j].ID })
	return hits, nil
}

// String is the tab-separated tail of the report line (ID, commit, path).
func (h SpecHit) String() string {
	return fmt.Sprintf("%s\t%s\t%s", h.ID, h.Commit, h.Path)
}

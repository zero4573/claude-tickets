// Package diffcheck flags binary or oversized files that any commit of a
// revision range adds or modifies, so a stray build output is caught before
// a branch is merged.
package diffcheck

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/zero4573/claude-tickets/internal/gitx"
)

// DefaultMaxSize is the size limit when none is given (1 MiB).
const DefaultMaxSize int64 = 1 << 20

// Finding is one flagged (commit, path).
type Finding struct {
	Kind   string // "binary", "large" or "binary,large"
	Size   int64  // blob size in bytes
	Commit string // short sha
	Path   string
}

// ParseSize reads a byte count or a size with a K, M or G suffix (powers
// of 1024, case-insensitive).
func ParseSize(s string) (int64, error) {
	t := strings.TrimSpace(s)
	mult := int64(1)
	if t != "" {
		switch t[len(t)-1] {
		case 'k', 'K':
			mult = 1 << 10
		case 'm', 'M':
			mult = 1 << 20
		case 'g', 'G':
			mult = 1 << 30
		}
		if mult != 1 {
			t = t[:len(t)-1]
		}
	}
	n, err := strconv.ParseInt(t, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("bad size '%s' (a byte count, or a number with K, M or G)", s)
	}
	if n > math.MaxInt64/mult {
		return 0, fmt.Errorf("size '%s' is too large (over %d bytes)", s, int64(math.MaxInt64))
	}
	return n * mult, nil
}

type entry struct {
	commit, path, blob string
	binary             bool
}

// Check lists the files that the non-merge commits of revRange add or
// modify (deletions are ignored) and that are binary or larger than
// maxSize bytes, in rev-list order, then path order. Read-only.
func Check(dir, revRange string, maxSize int64) ([]Finding, error) {
	revs, err := Revs(dir, revRange)
	if err != nil {
		return nil, err
	}
	var entries []entry
	for _, sha := range revs {
		es, err := commitEntries(dir, sha)
		if err != nil {
			return nil, err
		}
		entries = append(entries, es...)
	}
	if len(entries) == 0 {
		return nil, nil
	}
	var in strings.Builder
	for _, e := range entries {
		in.WriteString(e.blob + "\n")
	}
	sizes, err := gitx.OutIn(dir, strings.NewReader(in.String()), "cat-file", "--batch-check")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(sizes, "\n")
	if len(lines) != len(entries) {
		return nil, fmt.Errorf("cat-file returned %d lines for %d blobs", len(lines), len(entries))
	}
	var res []Finding
	for i, e := range entries {
		f := strings.Fields(lines[i])
		if len(f) != 3 || f[1] != "blob" {
			return nil, fmt.Errorf("cannot size %s (%s)", e.path, lines[i])
		}
		size, err := strconv.ParseInt(f[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot size %s (%s)", e.path, lines[i])
		}
		large := size > maxSize
		if !e.binary && !large {
			continue
		}
		kind := "large"
		switch {
		case e.binary && large:
			kind = "binary,large"
		case e.binary:
			kind = "binary"
		}
		res = append(res, Finding{Kind: kind, Size: size, Commit: ShortSHA(e.commit), Path: e.path})
	}
	return res, nil
}

// ShortSHA is the short form of a sha used in Finding and IDHit.
func ShortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// commitEntries returns the added or modified files of one commit against
// its first parent (the empty tree for a root commit).
func commitEntries(dir, sha string) ([]entry, error) {
	out, err := gitx.OutIn(dir, nil, "diff-tree", "-r", "--root", "--no-renames", "--no-commit-id", "-z", "--raw", "--numstat", sha)
	if err != nil {
		return nil, err
	}
	var raw []entry
	binary := map[string]bool{}
	toks := strings.Split(out, "\x00")
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t == "" {
			continue
		}
		if t[0] == ':' && i+1 < len(toks) {
			f := strings.Fields(t[1:])
			path := toks[i+1]
			i++
			if len(f) < 5 {
				return nil, fmt.Errorf("unexpected diff-tree record '%s'", t)
			}
			status, mode, blob := f[4], f[1], f[3]
			if status[0] == 'D' || mode == "160000" {
				continue
			}
			raw = append(raw, entry{commit: sha, path: path, blob: blob})
			continue
		}
		if p := strings.SplitN(t, "\t", 3); len(p) == 3 && p[0] == "-" && p[1] == "-" {
			binary[p[2]] = true
		}
	}
	for i := range raw {
		raw[i].binary = binary[raw[i].path]
	}
	return raw, nil
}

package diffcheck

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// A wave is named on the board's task lines (`· w<N> ·`) and in its
// review files (reviews/wave-<N>-<slug>.md).
var (
	boardWaveRe  = regexp.MustCompile(`(?m)^- \[.\] .* · w([0-9]+) · `)
	reviewWaveRe = regexp.MustCompile(`^wave-([0-9]+)-`)
	// a test identifier built from a wave: TestW116Foo, test_w3_bar,
	// testW12 (the number is checked against the ticket's waves)
	waveTestRe = regexp.MustCompile(`\b\w*?[Tt]est_?[Ww]([0-9]+)\w*`)
)

// TicketWaves is the set of the ticket's wave numbers, read from its
// folder: board.md's (and board-archive.md's) task lines and the review
// files' names. Sorted, unique.
func TicketWaves(folder string) []int {
	set := map[int]bool{}
	add := func(s string) {
		if n, err := strconv.Atoi(s); err == nil {
			set[n] = true
		}
	}
	for _, f := range []string{"board.md", "board-archive.md"} {
		b, err := os.ReadFile(filepath.Join(folder, f))
		if err != nil {
			continue
		}
		for _, m := range boardWaveRe.FindAllStringSubmatch(string(b), -1) {
			add(m[1])
		}
	}
	reviews, _ := filepath.Glob(filepath.Join(folder, "reviews", "wave-*.md"))
	for _, f := range reviews {
		if m := reviewWaveRe.FindStringSubmatch(filepath.Base(f)); m != nil {
			add(m[1])
		}
	}
	out := make([]int, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// waveTestMatcher counts test identifiers that embed one of the waves.
type waveTestMatcher struct{ waves map[int]bool }

func (m waveTestMatcher) count(s string, into map[string]int) {
	for _, sm := range waveTestRe.FindAllStringSubmatch(s, -1) {
		if n, err := strconv.Atoi(sm[1]); err == nil && m.waves[n] {
			into[sm[0]]++
		}
	}
}

// isTestPath is a file that holds tests in the usual layouts: Go's
// _test.go, Python's test_*.py and *_test.py, *.test.* and *.spec.*
// (JavaScript, TypeScript), *_spec.rb, Elixir's *_test.exs, Dart's
// *_test.dart, or anything under a test/, tests/, __tests__/ or spec/
// directory.
func isTestPath(p string) bool {
	base := path.Base(p)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	switch {
	case strings.HasSuffix(stem, "_test"), strings.HasSuffix(stem, "_spec"),
		strings.HasPrefix(base, "test_") && ext == ".py",
		strings.HasSuffix(stem, ".test"), strings.HasSuffix(stem, ".spec"):
		return true
	}
	for _, seg := range strings.Split(path.Dir(p), "/") {
		switch seg {
		case "test", "tests", "__tests__", "spec":
			return true
		}
	}
	return false
}

// CheckWaveTests lists the test identifiers named after one of waves that
// the non-merge commits of revRange introduce in test files (a name the
// commit adds more often than it removes, so one kept from the base
// passes), in rev-list order, then path, then name. Commit messages are
// not checked. Read-only.
func CheckWaveTests(dir, revRange string, waves []int) ([]IDHit, error) {
	if len(waves) == 0 {
		return nil, nil
	}
	revs, err := Revs(dir, revRange)
	if err != nil {
		return nil, err
	}
	m := waveTestMatcher{waves: map[int]bool{}}
	for _, n := range waves {
		m.waves[n] = true
	}
	var hits []IDHit
	for _, sha := range revs {
		h, err := commitIDs(dir, sha, m, false, isTestPath)
		if err != nil {
			return nil, err
		}
		hits = append(hits, h...)
	}
	return hits, nil
}

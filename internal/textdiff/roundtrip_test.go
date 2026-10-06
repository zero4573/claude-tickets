package textdiff

import (
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@\n$`)

// patch applies a unified diff made by Unified to a, checking every context
// and deleted line and each hunk's counts, as patch(1) would.
func patch(t *testing.T, a, diff string) string {
	t.Helper()
	if diff == "" {
		return a
	}
	al := lines([]byte(a))
	dl := strings.SplitAfter(diff, "\n")
	if dl[len(dl)-1] == "" {
		dl = dl[:len(dl)-1]
	}
	if len(dl) < 3 || !strings.HasPrefix(dl[0], "--- ") || !strings.HasPrefix(dl[1], "+++ ") {
		t.Fatalf("no --- / +++ header:\n%s", diff)
	}
	num := func(s string, def int) int {
		if s == "" {
			return def
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	var out []string
	pos := 0
	for i := 2; i < len(dl); {
		m := hunkHeader.FindStringSubmatch(dl[i])
		if m == nil {
			t.Fatalf("line %d isn't a hunk header: %q\n%s", i+1, dl[i], diff)
		}
		aStart, aCount := num(m[1], 0), num(m[2], 1)
		bCount := num(m[4], 1)
		start := aStart - 1
		if aCount == 0 {
			start = aStart
		}
		if start < pos || start > len(al) {
			t.Fatalf("hunk %q out of order or range (at %d of %d)", dl[i], pos, len(al))
		}
		out = append(out, al[pos:start]...)
		pos = start
		i++
		var last byte
		gotA, gotB := 0, 0
		for ; i < len(dl) && !strings.HasPrefix(dl[i], "@@ "); i++ {
			l := dl[i]
			if strings.HasPrefix(l, `\ No newline at end of file`) {
				// the previous line has no "\n"
				if last != '-' {
					out[len(out)-1] = strings.TrimSuffix(out[len(out)-1], "\n")
				}
				continue
			}
			last = l[0]
			text := l[1:]
			switch l[0] {
			case ' ', '-':
				if pos >= len(al) || strings.TrimSuffix(al[pos], "\n") != strings.TrimSuffix(text, "\n") {
					t.Fatalf("hunk line %q doesn't match a's line %d", l, pos+1)
				}
				pos++
				gotA++
				if l[0] == ' ' {
					out = append(out, text)
					gotB++
				}
			case '+':
				out = append(out, text)
				gotB++
			default:
				t.Fatalf("unexpected diff line %q", l)
			}
		}
		if gotA != aCount || gotB != bCount {
			t.Fatalf("hunk %q has %d/%d lines, header says %d/%d", m[0], gotA, gotB, aCount, bCount)
		}
	}
	return strings.Join(append(out, al[pos:]...), "")
}

func randomText(r *rand.Rand) string {
	n := r.Intn(25)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(string(rune('a' + r.Intn(4))))
		if i < n-1 || r.Intn(4) != 0 { // sometimes no final newline
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// mutate makes b from a with a few insertions, deletions and changes, so
// the two share most lines, as an edited vault file and the shipped one do.
func mutate(r *rand.Rand, a string) string {
	ls := lines([]byte(a))
	for k := r.Intn(5); k > 0; k-- {
		switch at := r.Intn(len(ls) + 1); r.Intn(3) {
		case 0:
			ls = append(ls[:at], append([]string{"new" + strconv.Itoa(k) + "\n"}, ls[at:]...)...)
		case 1:
			if at < len(ls) {
				ls = append(ls[:at], ls[at+1:]...)
			}
		case 2:
			if at < len(ls) && strings.HasSuffix(ls[at], "\n") {
				ls[at] = "x" + ls[at]
			}
		}
	}
	return strings.Join(ls, "")
}

// TestUnifiedRoundTrip: applying the diff of a to b turns a into b, for
// random texts and every context size; equal texts give "".
func TestUnifiedRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	for n := 0; n < 3000; n++ {
		a := randomText(r)
		b := randomText(r)
		if n%2 == 0 {
			b = mutate(r, a)
		}
		for _, ctx := range []int{0, 1, 3} {
			d := Unified("a", "b", []byte(a), []byte(b), ctx)
			if (d == "") != (a == b) {
				t.Fatalf("a=%q b=%q context %d: diff %q", a, b, ctx, d)
			}
			if got := patch(t, a, d); got != b {
				t.Fatalf("a=%q b=%q context %d: patched to %q\n%s", a, b, ctx, got, d)
			}
		}
	}
}

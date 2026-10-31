package diffcheck

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTicketWaves(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "reviews"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"board.md": "## Tasks\n- [x] 01 · merged · [[PROJ-12-01-a]] · acme · w1 · deps - · a\n" +
			"- [ ] 02 · planned · [[PROJ-12-02-b]] · acme · w3 · deps 01 · b\n" +
			"prose about w9 is no task line\n",
		"board-archive.md":          "- [x] 00 · merged · [[PROJ-12-00-z]] · acme · w12 · deps - · z\n",
		"reviews/wave-5-acme.md":    "",
		"reviews/notes.md":          "",
		"reviews/wave-x-unnamed.md": "",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := TicketWaves(dir), []int{1, 3, 5, 12}; !reflect.DeepEqual(got, want) {
		t.Errorf("TicketWaves = %v, want %v", got, want)
	}
	if got := TicketWaves(t.TempDir()); len(got) != 0 {
		t.Errorf("TicketWaves of an empty folder = %v", got)
	}
}

func TestWaveTestMatcher(t *testing.T) {
	m := waveTestMatcher{waves: map[int]bool{3: true, 116: true}}
	got := map[string]int{}
	for _, l := range []string{
		"func TestW116DeleteConflict(t *testing.T) {",
		"def test_w3_retries():",
		"  it('x', () => { testW3() })",
		"func TestW1162Other(t *testing.T) {}", // wave 1162 isn't one
		"func TestW2Form(t *testing.T) {}",     // nor 2
		"func TestWorker(t *testing.T) {}",
		"W116 in a comment is no test name",
	} {
		m.count(l, got)
	}
	want := map[string]int{"TestW116DeleteConflict": 1, "test_w3_retries": 1, "testW3": 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("count = %v, want %v", got, want)
	}
}

func TestIsTestPath(t *testing.T) {
	for p, want := range map[string]bool{
		"internal/a/a_test.go":         true,
		"pkg/test_orders.py":           true,
		"pkg/orders_test.py":           true,
		"src/orders.test.ts":           true,
		"src/orders.spec.tsx":          true,
		"spec/models/order_spec.rb":    true,
		"test/orders_test.exs":         true,
		"lib/orders_test.dart":         true,
		"src/__tests__/orders.js":      true,
		"tests/fixtures/data.json":     true,
		"internal/a/a.go":              false,
		"docs/testing.md":              false,
		"src/latest.ts":                false,
		"pkg/contest/contest_entry.py": false,
	} {
		if got := isTestPath(p); got != want {
			t.Errorf("isTestPath(%q) = %v, want %v", p, got, want)
		}
	}
}

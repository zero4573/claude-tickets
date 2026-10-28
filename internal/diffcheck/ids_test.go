package diffcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestIDsSet(t *testing.T) {
	d := t.TempDir()
	must := func(p, s string) {
		full := filepath.Join(d, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// entry lines only: a mention in prose (R-77, B-9, W2-x-S1) doesn't count
	must("relay.md", "R-9093 · 2026-01-02 · user→lead · decision · as R-77 said, not B-9 · copied\nR-4 · q\n<!-- R-nn · format -->\nsee R-77 · prose\n")
	must("backlog.md", "- [ ] B-9092 · w1 · a\n- [x] B-9091 · w2 · b (B-9)\nB-8 · not an item\n")
	must("reviews/wave-1-x.md", "- W1-acme-svc-M01 · a · W2-x-S1\n- [/] W12-x-S3 · fixing\nW3-x-C01 in prose\n")
	must("tasks/t.md", "R-77 · x\n")
	got := TicketIDs(d, "PROJ-12")
	want := []string{"B-9091", "B-9092", "PROJ-12", "R-4", "R-9093", "W1-acme-svc-M01", "W12-x-S3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if got := TicketIDs(filepath.Join(d, "none"), "PROJ-12"); !reflect.DeepEqual(got, []string{"PROJ-12"}) {
		t.Fatalf("missing folder: %v", got)
	}
}

func TestIDsCheck(t *testing.T) {
	d := DiffCheckRepo(t)
	DiffCheckCommit(t, d, "init", map[string][]byte{"a.txt": []byte("x\n")})
	DiffCheckGit(t, d, "checkout", "-q", "-b", "topic")
	c1 := DiffCheckCommit(t, d, "fix for R-9093", map[string][]byte{
		"b.txt":   []byte("see PROJ-12 and R-9090 and R-90931\nPROJ-12 again R-9093\n"),
		"bin.dat": []byte("PROJ-12\x00\x01"),
		"c.txt":   []byte("clean\n"),
	})
	// a deleted line mentioning an ID is not an added line
	DiffCheckCommit(t, d, "edit", map[string][]byte{"a.txt": []byte("y\n"), "b.txt": []byte("clean now\n")})
	hits, err := CheckIDs(d, "main..topic", []string{"PROJ-12", "R-9093"})
	if err != nil {
		t.Fatal(err)
	}
	want := []IDHit{
		{"R-9093", c1, MessagePath},
		{"PROJ-12", c1, "b.txt"},
		{"R-9093", c1, "b.txt"},
	}
	if !reflect.DeepEqual(hits, want) {
		t.Fatalf("got %v want %v", hits, want)
	}
	if hits, _ := CheckIDs(d, "main..topic", []string{"R-9090x"}); len(hits) != 0 {
		t.Fatalf("unexpected %v", hits)
	}
	if _, err := CheckIDs(d, "--all", nil); err == nil {
		t.Fatal("option range accepted")
	}
}

func TestIDsKeptFromBase(t *testing.T) {
	d := DiffCheckRepo(t)
	DiffCheckCommit(t, d, "init", map[string][]byte{"k.md": []byte("Ask user: none | R-9093: q\nother\n")})
	DiffCheckGit(t, d, "checkout", "-q", "-b", "topic")
	// a modified line that keeps its ID passes
	DiffCheckCommit(t, d, "reword", map[string][]byte{"k.md": []byte("Ask user: none | R-9093: question\nother\n")})
	if hits, err := CheckIDs(d, "main..topic", []string{"R-9093"}); err != nil || len(hits) != 0 {
		t.Fatalf("kept ID flagged: %v %v", hits, err)
	}
	// a second occurrence is introduced
	c := DiffCheckCommit(t, d, "more", map[string][]byte{"k.md": []byte("Ask user: none | R-9093: question\nalso R-9093\n")})
	hits, err := CheckIDs(d, "main..topic", []string{"R-9093"})
	if want := []IDHit{{"R-9093", c, "k.md"}}; err != nil || !reflect.DeepEqual(hits, want) {
		t.Fatalf("got %v %v want %v", hits, err, want)
	}
	// two IDs side by side are two whole words
	c2 := DiffCheckCommit(t, d, "pair", map[string][]byte{"p.md": []byte("R-9093 R-9094,R-9095\n")})
	hits, _ = CheckIDs(d, c+".."+c2, []string{"R-9093", "R-9094", "R-9095", "R-90"})
	want := []IDHit{{"R-9093", c2, "p.md"}, {"R-9094", c2, "p.md"}, {"R-9095", c2, "p.md"}}
	if !reflect.DeepEqual(hits, want) {
		t.Fatalf("got %v want %v", hits, want)
	}
}

func TestIDsSpecialPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tab and quote are not valid in Windows file names")
	}
	d := DiffCheckRepo(t)
	DiffCheckCommit(t, d, "init", map[string][]byte{"r.txt": []byte("x\n")})
	c := DiffCheckCommit(t, d, "add", map[string][]byte{
		"a b.txt":   []byte("R-9091\n"),
		"q\"x.txt":  []byte("R-9092\n"),
		"t\tab.txt": []byte("R-9093\n"),
		"é.txt":     []byte("R-9094\n"),
	})
	hits, err := CheckIDs(d, "HEAD~1..HEAD", []string{"R-9091", "R-9092", "R-9093", "R-9094"})
	want := []IDHit{{"R-9091", c, "a b.txt"}, {"R-9092", c, "q\"x.txt"}, {"R-9093", c, "t\tab.txt"}, {"R-9094", c, "é.txt"}}
	if err != nil || !reflect.DeepEqual(hits, want) {
		t.Fatalf("got %q %v want %q", hits, err, want)
	}
}

func BenchmarkIDsCheck(b *testing.B) {
	d := DiffCheckRepo(b)
	DiffCheckCommit(b, d, "init", map[string][]byte{"r.txt": []byte("x\n")})
	var sb strings.Builder
	for i := 0; i < 100000; i++ {
		fmt.Fprintf(&sb, "line %d of generated text with no ids in it\n", i)
	}
	DiffCheckCommit(b, d, "big", map[string][]byte{"gen.txt": []byte(sb.String())})
	ids := make([]string, 60)
	for i := range ids {
		ids[i] = fmt.Sprintf("R-%d", 9000+i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := CheckIDs(d, "HEAD~1..HEAD", ids); err != nil {
			b.Fatal(err)
		}
	}
}

func TestIDsTypeChangeAndRename(t *testing.T) {
	d := DiffCheckRepo(t)
	DiffCheckCommit(t, d, "init", map[string][]byte{"t.md": []byte("R-9093\n"), "a.md": []byte("R-9094\n")})
	DiffCheckGit(t, d, "checkout", "-q", "-b", "topic")
	// a file turned into a symlink: two diff blocks for one path
	if err := os.Remove(filepath.Join(d, "t.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.md", filepath.Join(d, "t.md")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	DiffCheckGit(t, d, "add", "-A")
	DiffCheckGit(t, d, "commit", "-q", "-m", "link")
	// a rename keeps its ID
	DiffCheckGit(t, d, "mv", "a.md", "b.md")
	DiffCheckGit(t, d, "commit", "-q", "-m", "move")
	if hits, err := CheckIDs(d, "main..topic", []string{"R-9093", "R-9094"}); err != nil || len(hits) != 0 {
		t.Fatalf("got %v %v, want no hits", hits, err)
	}
}

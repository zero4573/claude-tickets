package vbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zero4573/claude-tickets/internal/chunk"
	"github.com/zero4573/claude-tickets/internal/modelcache"
	"github.com/zero4573/claude-tickets/internal/vindex"
	"github.com/zero4573/claude-tickets/internal/vsearch"
)

// TestReport prints the benchmark report: CT_BENCH=1 go test ./internal/vbench -run TestReport -v
// (make bench). CT_EMBED_REAL=1 uses the real model when it is fetched.
func TestReport(t *testing.T) {
	if os.Getenv("CT_BENCH") == "" {
		t.Skip("set CT_BENCH=1 to run the benchmark")
	}
	embedder := "stub"
	if os.Getenv("CT_EMBED_REAL") == "1" {
		st := modelcache.StatusOf(modelcache.Default().ID)
		if st.ModelOK && st.LibOK {
			embedder = st.Model.ID
			t.Setenv("CLAUDE_TICKETS_EMBEDDER", "")
		} else {
			fmt.Printf("CT_EMBED_REAL=1 but the model is not fetched (%s); using the stub\n", st.Problem)
		}
	}
	if embedder == "stub" {
		t.Setenv("CLAUDE_TICKETS_EMBEDDER", "stub")
	}
	dir := t.TempDir()
	v, err := Generate(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	st, err := vindex.Update(dir, vindex.UpdateOptions{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	st2, err := vindex.Update(dir, vindex.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	emb, err := modelcache.Open(modelcache.Default().ID)
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	files := mdFiles(dir)

	type tot struct{ n, old, new, oldOK, newOK int }
	scen := map[string]*tot{"kb": {}, "recall": {}}
	var lat []time.Duration
	followUps := 0
	for _, f := range v.Facts {
		var resp *vsearch.Response
		for rep := 0; rep < 3; rep++ {
			t0 := time.Now()
			ix, err := vindex.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = vsearch.Search(ix, emb, f.Question, vsearch.Options{K: 10}); err != nil {
				t.Fatal(err)
			}
			lat = append(lat, time.Since(t0))
		}
		ix, err := vindex.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		resp, err = vsearch.Search(ix, emb, f.Question, SearchOptions(f))
		if err != nil {
			t.Fatal(err)
		}
		o := Old(dir, files, f)
		n, fu := New(dir, resp, f)
		if fu {
			followUps++
		}
		s := scen[f.Scenario]
		s.n++
		s.old += o.Tokens
		s.new += n.Tokens
		if o.Found {
			s.oldOK++
		}
		if n.Found {
			s.newOK++
		}
		fmt.Printf("  %-6s old=%6d tok found=%-5v new=%5d tok found=%-5v  %s\n", f.Scenario, o.Tokens, o.Found, n.Tokens, n.Found, f.Question)
	}
	fmt.Printf("\nembedder: %s   notes: %d   chunks: %d\n", embedder, st.Notes, st.Chunks)
	fmt.Printf("index build: full %s (embedded %d), no-op re-run %s\n", st.Took.Round(time.Millisecond), st.Embedded, st2.Took.Round(time.Millisecond))
	fmt.Printf("top-10 search incl. Load: p50 %s  p95 %s  (%d runs)\n\n", Percentile(lat, .5).Round(time.Millisecond), Percentile(lat, .95).Round(time.Millisecond), len(lat))
	fmt.Printf("%-8s %3s %10s %10s %10s %9s %9s\n", "scenario", "n", "old tok", "new tok", "reduction", "old qual", "new qual")
	var all tot
	for _, k := range []string{"kb", "recall"} {
		s := scen[k]
		fmt.Printf("%-8s %3d %10d %10d %9.1f%% %6d/%-2d %6d/%-2d\n", k, s.n, s.old, s.new, 100*(1-float64(s.new)/float64(s.old)), s.oldOK, s.n, s.newOK, s.n)
		all.n += s.n
		all.old += s.old
		all.new += s.new
		all.oldOK += s.oldOK
		all.newOK += s.newOK
	}
	red := 100 * (1 - float64(all.new)/float64(all.old))
	fmt.Printf("%-8s %3d %10d %10d %9.1f%% %6d/%-2d %6d/%-2d\n", "all", all.n, all.old, all.new, red, all.oldOK, all.n, all.newOK, all.n)
	fmt.Printf("new counts %d follow-up `note get` call(s) of the top section (made when the search text lacked the answer)\n", followUps)
	note := ""
	if embedder == "stub" {
		// The stub has no semantics: its quality is the keyword-only floor and
		// the targets are a regression guard (budget, latency), not the
		// product's numbers.
		note = " [keyword-only floor: the stub has no semantics; CT_EMBED_REAL=1 gives the real numbers]"
	}
	fmt.Printf("\ntargets (%s)%s: >=50%% fewer tokens: %v; search < 1s (p95): %v; quality new >= old: %v\n", embedder, note,
		red >= 50, Percentile(lat, .95) < time.Second, all.newOK >= all.oldOK)
	if embedder != "stub" && (red < 50 || all.newOK < all.oldOK) {
		t.Errorf("targets missed with the real model")
	}
}

// W4 S02: New's cost includes the follow-up `note get` of the top section when
// the search text lacks the answer; and OLD with one distinctive keyword reads
// at most ReadN notes (W4 S01).
func TestNewCountsFollowUp(t *testing.T) {
	dir := t.TempDir()
	long := "# Top\n\n## Intro\n\nShort.\n\n## Answer\n\nThe planted sentence lives here and is long enough to count. " + strings.Repeat("More words follow. ", 40) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "top.md"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	f := Fact{Sentence: "The planted sentence lives here"}
	resp := &vsearch.Response{Results: []vsearch.Result{{Rank: 1, Path: "top.md", Heading: []string{"Top", "Answer"}, Text: "unrelated text"}}}
	n, fu := New(dir, resp, f)
	base := chunk.Tokens(Render(resp))
	if !fu || n.Tokens <= base || !n.Found {
		t.Fatalf("follow-up not counted: %+v follow=%v base=%d", n, fu, base)
	}
	resp.Results[0].Text = f.Sentence
	if n, fu = New(dir, resp, f); fu || n.Tokens != chunk.Tokens(Render(resp)) {
		t.Fatalf("follow-up made though the search had the answer: %+v", n)
	}
}

func TestOldReadsAtMostReadN(t *testing.T) {
	dir := t.TempDir()
	var files []string
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("n%02d.md", i)
		files = append(files, name)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# N\n\nmentions zebra here\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f := Fact{Scenario: "kb", Keywords: []string{"acme", "zebra", "the"}, Sentence: "nope"}
	if d := Distinctive(f); len(d) != 1 || d[0] != "zebra" {
		t.Fatalf("distinctive %v", d)
	}
	one := chunk.Tokens(read(dir, "n00.md"))
	if o := Old(dir, files, f); o.Tokens > ReadN*one+chunk.Tokens(listing(dir, "projects", "knowledge-base", "references"))+12*8 {
		t.Fatalf("read more than %d notes: %d tokens", ReadN, o.Tokens)
	}
}

func TestDistinctiveFallsBackToAllKeywords(t *testing.T) {
	f := Fact{Keywords: []string{"The", "of"}}
	if d := Distinctive(f); len(d) != 2 || d[0] != "the" {
		t.Fatalf("no fallback: %v", d)
	}
}

func TestListsProjectAmongSeveral(t *testing.T) {
	for note, want := range map[string]bool{
		"---\nprojects: [acme]\n---\n":              true,
		"---\nprojects: [beta, acme, gamma]\n---\n": true,
		"---\nprojects: [acme-two, beta]\n---\n":    false,
		"---\ntags: [acme]\n---\n":                  false,
	} {
		if got := listsProject(note, "acme"); got != want {
			t.Errorf("%q: got %v", note, got)
		}
	}
}

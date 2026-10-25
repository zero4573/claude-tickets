package vbench

import (
	"fmt"
	"os"
	"testing"
	"time"

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
		o, n := Old(dir, files, f), New(resp, f)
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
	fmt.Printf("\ntargets (%s): >=50%% fewer tokens: %v; search < 1s (p95): %v; quality new >= old: %v\n", embedder,
		red >= 50, Percentile(lat, .95) < time.Second, all.newOK >= all.oldOK)
}

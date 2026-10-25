package vsearch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zero4573/claude-tickets/internal/embed"
	"github.com/zero4573/claude-tickets/internal/vindex"
)

func put(t *testing.T, vault, rel, body string) {
	t.Helper()
	p := filepath.Join(vault, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// remap embeds a query as if it were another text: a stand-in for meaning.
type remap struct {
	embed.Embedder
	to string
}

func (r remap) Embed(texts []string, query bool) ([][]float32, error) {
	return r.Embedder.Embed([]string{r.to}, query)
}

func fixture(t *testing.T) *vindex.Index {
	t.Helper()
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "stub")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	v := t.TempDir()
	put(t, v, "projects/acme/acme.md", "---\ntype: project\ntags: [core]\n---\n# Acme\n\nGiraffe savanna migration patterns.\n\nSee [[crossing]] and [[linked]].\n")
	put(t, v, "projects/acme/crossing.md", "---\ntype: note\ntags: [misc]\nprojects: [acme]\n---\n# Crossing\n\nZebra crossing paint rules.\n")
	put(t, v, "projects/acme/linked.md", "---\ntype: note\n---\n# Linked\n\nUnrelated words but mentions [[acme]].\n")
	put(t, v, "other/far.md", "---\ntype: note\ntags: [misc]\n---\n# Far\n\nQuartz basalt granite.\n")
	if _, err := vindex.Update(v, vindex.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	ix, err := vindex.Load(v)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func paths(r *Response) []string {
	var out []string
	for _, x := range r.Results {
		out = append(out, x.Path)
	}
	return out
}

func TestHybridPrefersSemanticOverKeywordOnly(t *testing.T) {
	ix := fixture(t)
	emb := remap{embed.Stub(64), "# Acme\n\nGiraffe savanna migration patterns."}
	r, err := Search(ix, emb, "zebra crossing", Options{NoLinks: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.KeywordOnly || r.Results[0].Path != "projects/acme/acme.md" {
		t.Fatalf("hybrid: %v keyword_only=%v", paths(r), r.KeywordOnly)
	}
	if r.Results[0].Scores.Semantic <= 0 {
		t.Fatalf("scores %+v", r.Results[0].Scores)
	}
	k, err := Search(ix, emb, "zebra crossing", Options{Mode: ModeKeyword, NoLinks: true})
	if err != nil || k.Results[0].Path != "projects/acme/crossing.md" || !k.KeywordOnly {
		t.Fatalf("keyword: %v %v", err, paths(k))
	}
	s, err := Search(ix, emb, "zebra crossing", Options{Mode: ModeSemantic, NoLinks: true})
	if err != nil || s.Results[0].Path != "projects/acme/acme.md" || s.Results[0].Scores.Keyword != 0 || s.KeywordOnly {
		t.Fatalf("semantic: %v", err)
	}
}

func TestNoModel(t *testing.T) {
	ix := fixture(t)
	r, err := Search(ix, nil, "zebra", Options{NoLinks: true})
	if err != nil || !r.KeywordOnly || len(r.Results) != 1 || r.Results[0].Note != "crossing" {
		t.Fatalf("%v %+v", err, r)
	}
	if _, err := Search(ix, nil, "zebra", Options{Mode: ModeSemantic}); err != ErrNoModel {
		t.Fatalf("err %v", err)
	}
}

func TestFilters(t *testing.T) {
	ix := fixture(t)
	q := "zebra giraffe quartz words acme crossing far linked"
	cases := []struct {
		name string
		o    Options
		want string
	}{
		{"path", Options{Paths: []string{"other"}}, "other/far.md"},
		{"paths or", Options{Paths: []string{"other", "projects/acme/crossing.md"}}, "other/far.md projects/acme/crossing.md"},
		{"type", Options{Type: "project"}, "projects/acme/acme.md"},
		{"project", Options{Project: "acme"}, "projects/acme/acme.md projects/acme/crossing.md projects/acme/linked.md"},
		{"tag", Options{Tag: "misc"}, "other/far.md projects/acme/crossing.md"},
		{"and", Options{Tag: "misc", Project: "acme"}, "projects/acme/crossing.md"},
	}
	for _, c := range cases {
		c.o.Mode, c.o.NoLinks = ModeKeyword, true
		r, err := Search(ix, nil, q, c.o)
		if err != nil {
			t.Fatal(err)
		}
		got := paths(r)
		if !sameSet(got, strings.Fields(c.want)) {
			t.Errorf("%s: got %v want %s", c.name, got, c.want)
		}
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]bool{}
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		if !m[x] {
			return false
		}
	}
	return true
}

func TestBudgetAndExcerpt(t *testing.T) {
	ix := fixture(t)
	q := "zebra giraffe quartz words acme crossing far linked"
	all, _ := Search(ix, nil, q, Options{NoLinks: true, K: 10})
	if len(all.Results) < 3 || all.Budget != nil || all.Results[0].Text != "" {
		t.Fatalf("all %v", paths(all))
	}
	tok := all.Results[0].Tokens
	r, _ := Search(ix, nil, q, Options{NoLinks: true, Budget: tok + 1})
	if len(r.Results) != 1 || !r.Budget.Truncated || r.Budget.Used != tok || r.Results[0].Text == "" {
		t.Fatalf("cut: %+v %+v", r.Results, r.Budget)
	}
	r, _ = Search(ix, nil, q, Options{NoLinks: true, Budget: 1})
	if len(r.Results) != 1 {
		t.Fatalf("at least one: %d", len(r.Results))
	}
	r, _ = Search(ix, nil, q, Options{NoLinks: true, Budget: 100000})
	if r.Budget.Truncated || len(r.Results) != len(all.Results) {
		t.Fatalf("roomy: %+v", r.Budget)
	}
	if e := excerpt("a\n\n  b  " + strings.Repeat("x", 400)); strings.Contains(e, "\n") || len([]rune(e)) > ExcerptMax {
		t.Fatalf("excerpt %q", e)
	}
}

func TestLinkBoost(t *testing.T) {
	ix := fixture(t)
	// The boost only lifts chunks that already match; one seed note: acme.
	defer func(n int) { linkSeeds = n }(linkSeeds)
	linkSeeds = 1
	on, _ := Search(ix, nil, "crossing see words", Options{Mode: ModeKeyword})
	off, _ := Search(ix, nil, "crossing see words", Options{Mode: ModeKeyword, NoLinks: true})
	boosted := false
	for _, r := range on.Results {
		if r.Scores.Link > 0 {
			boosted = true
			if r.Note == "acme" {
				t.Fatalf("boosted %s", r.Note)
			}
		}
	}
	if !boosted {
		t.Fatalf("no boost: %+v", on.Results)
	}
	for _, r := range off.Results {
		if r.Scores.Link != 0 {
			t.Fatal("boost with NoLinks")
		}
	}
}

func TestRelated(t *testing.T) {
	ix := fixture(t)
	emb := embed.Stub(64)
	acme := NoteIndex(ix, "projects/acme/acme.md")
	r, err := Related(ix, emb, acme, RelatedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range r.Results {
		if x.Note == "acme" || x.Note == "crossing" || x.Note == "linked" {
			t.Fatalf("not excluded: %+v", r.Results)
		}
	}
	r, _ = Related(ix, emb, acme, RelatedOptions{IncludeLinked: true})
	var sawLinked bool
	for _, x := range r.Results {
		if x.Note == "linked" && x.Linked {
			sawLinked = true
		}
		if x.Note == "acme" {
			t.Fatal("self listed")
		}
	}
	if !sawLinked {
		t.Fatalf("include-linked: %+v", r.Results)
	}
	c, _ := Related(ix, emb, acme, RelatedOptions{IncludeLinked: true, Chunks: true})
	if len(c.Results) == 0 || c.Results[0].StartLine == 0 || len(c.Results[0].Heading) == 0 {
		t.Fatalf("chunks %+v", c.Results)
	}
	k, _ := Related(ix, nil, NoteIndex(ix, "projects/acme/linked.md"), RelatedOptions{IncludeLinked: true})
	if !k.KeywordOnly || len(k.Results) == 0 || k.Results[0].Note != "acme" {
		t.Fatalf("keyword fallback %+v", k)
	}
}

// A note added while the model is unavailable has zero vector rows in an index
// that has vectors ("not embedded yet").
func TestZeroRowsScoredByKeyword(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "stub")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	v := t.TempDir()
	put(t, v, "a.md", "# A\n\nGiraffe savanna migration.\n")
	put(t, v, "b.md", "# B\n\nQuartz basalt granite.\n")
	put(t, v, "a2.md", "# Pangolin armor\n\nScales.\n")
	if _, err := vindex.Update(v, vindex.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	put(t, v, "z.md", "# Pangolin armor\n\nPangolin scales armor.\n")
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "none")
	if _, err := vindex.Update(v, vindex.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	ix, err := vindex.Load(v)
	if err != nil || ix.Meta.Dims == 0 {
		t.Fatalf("%v dims %d", err, ix.Meta.Dims)
	}
	emb := embed.Stub(64)
	for _, mode := range []string{ModeHybrid, ModeSemantic} {
		r, err := Search(ix, emb, "pangolin armor", Options{Mode: mode})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		// On one scale (W4 S03) an embedded note with the same words may rank
		// above it, but the zero row is still found, by keyword.
		found := false
		for _, x := range r.Results {
			if x.Path == "z.md" {
				found = x.Score > 0
			}
		}
		if !found {
			t.Fatalf("%s: zero row not found by keyword: %v", mode, paths(r))
		}
	}
	// Related on the unembedded note and towards it.
	z := NoteIndex(ix, "z.md")
	if r, _ := Related(ix, emb, z, RelatedOptions{IncludeLinked: true}); !r.KeywordOnly {
		t.Fatal("seed without centroid should be keyword-only")
	}
	a2 := NoteIndex(ix, "a2.md")
	r, err := Related(ix, emb, a2, RelatedOptions{IncludeLinked: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range r.Results {
		found = found || x.Path == "z.md"
	}
	if !found {
		t.Fatalf("note without centroid missing from related: %+v", r.Results)
	}
}

func TestZeroScoreScale(t *testing.T) {
	// W4 S03: an embedded chunk with sem 0.8 and no keyword score beats a zero
	// row with keyword score 0.5 (the semantic mean of a typical index is low).
	embedded := WeightSemantic*0.8 + WeightKeyword*0
	zero := zeroScore(WeightSemantic, WeightKeyword, 0.1, 0.5)
	if !(embedded > zero) {
		t.Fatalf("embedded %v <= zero row %v", embedded, zero)
	}
	if zeroScore(WeightSemantic, WeightKeyword, 0.3, 0) != 0 {
		t.Fatal("zero row without keyword match scored")
	}
	if zero >= 0.5 {
		t.Fatalf("zero row not scaled: %v", zero)
	}
}

// End to end: with the query equal to an embedded chunk's text, that chunk
// outranks a zero row that matches one query word.
func TestStrongSemanticBeatsMediocreZeroRow(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "stub")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	v := t.TempDir()
	put(t, v, "a.md", "# Armor\n\nPangolin scales armor plates.\n")
	put(t, v, "b.md", "# B\n\nQuartz basalt granite.\n")
	if _, err := vindex.Update(v, vindex.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	put(t, v, "z.md", "# Z\n\nPangolin.\n")
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "none")
	if _, err := vindex.Update(v, vindex.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	ix, err := vindex.Load(v)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Search(ix, embed.Stub(64), "Armor\n\nPangolin scales armor plates.", Options{})
	if err != nil || len(r.Results) == 0 || r.Results[0].Path != "a.md" {
		t.Fatalf("%v %v", err, paths(r))
	}
}

// W5 S01: score == ws*semantic + wk*keyword + link for zero and embedded rows.
func TestScoreEqualsComponentsForZeroRow(t *testing.T) {
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "stub")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	v := t.TempDir()
	put(t, v, "a.md", "# A\n\nGiraffe savanna migration.\n")
	put(t, v, "a2.md", "# Pangolin armor\n\nScales.\n")
	if _, err := vindex.Update(v, vindex.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	put(t, v, "z.md", "# Pangolin armor\n\nPangolin scales armor.\n")
	t.Setenv("CLAUDE_TICKETS_EMBEDDER", "none")
	if _, err := vindex.Update(v, vindex.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	ix, err := vindex.Load(v)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Search(ix, embed.Stub(64), "pangolin armor", Options{Mode: ModeHybrid})
	if err != nil {
		t.Fatal(err)
	}
	var sawZero, sawEmbedded bool
	for _, x := range r.Results {
		want := WeightSemantic*x.Scores.Semantic + WeightKeyword*x.Scores.Keyword + x.Scores.Link
		if d := x.Score - want; d > 1e-9 || d < -1e-9 {
			t.Errorf("%s: score %v != %v (%+v)", x.Path, x.Score, want, x.Scores)
		}
		if x.Path == "z.md" {
			sawZero = x.Scores.Semantic > 0
		} else {
			sawEmbedded = true
		}
	}
	if !sawZero || !sawEmbedded {
		t.Fatalf("need a zero row with imputed semantic and an embedded row: %v", paths(r))
	}
}

// W6 S01: negative cosines count as 0 in the mean, noVec rows are left out, and
// no embedded candidate gives 0.
func TestMeanSemanticClampsNegatives(t *testing.T) {
	sem := []float64{0.6, -0.9, 0.0, 0.5, 0.3}
	noVec := func(i int) bool { return i == 3 }
	// Candidates 0,1,2,4 are embedded: (0.6 + 0 + 0 + 0.3) / 4.
	if got, want := meanSemantic([]int{0, 1, 2, 3, 4}, sem, noVec), 0.225; got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("mean %v, want %v", got, want)
	}
	if got := meanSemantic([]int{3}, sem, noVec); got != 0 {
		t.Fatalf("no embedded candidate: %v", got)
	}
	if _, n := meanSemanticN([]int{1}, sem, noVec); n != 1 {
		t.Fatalf("a negative-cosine candidate must count: n=%d", n)
	}
}

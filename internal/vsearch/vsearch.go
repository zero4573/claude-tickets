// Package vsearch is hybrid retrieval over a vindex.Index: semantic cosine,
// BM25 and a link boost, with filters and a token budget (ct vault search),
// and related notes (ct vault related). The API is the vault-search-cli-v1
// contract's "ct vault search" and "ct vault related".
package vsearch

import (
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/zero4573/claude-tickets/internal/embed"
	"github.com/zero4573/claude-tickets/internal/vindex"
)

// Score weights and the link boost.
const (
	WeightSemantic = 0.6 // hybrid: share of the cosine similarity
	WeightKeyword  = 0.4 // hybrid: share of the min-max-normalised BM25
	LinkBoost      = 0.1 // added to chunks of notes linked with a seed note
	LinkSeeds      = 3   // top notes that act as seeds
	ExcerptMax     = 240 // excerpt length in characters
	DefaultK       = 10
)

var linkSeeds = LinkSeeds

// Modes.
const (
	ModeHybrid   = "hybrid"
	ModeSemantic = "semantic"
	ModeKeyword  = "keyword"
)

// ErrNoModel means semantic search was asked for but there are no vectors or
// no usable embedder.
var ErrNoModel = errors.New("semantic search needs an embedding model")

// Options are the search parameters.
type Options struct {
	K       int      // 0 → DefaultK
	Paths   []string // vault-relative folder prefixes (OR)
	Type    string
	Project string
	Tag     string
	Budget  int    // 0 = none
	Mode    string // "" → hybrid
	NoLinks bool
}

// Scores are the parts of a result's score.
type Scores struct {
	Semantic float64 `json:"semantic"`
	Keyword  float64 `json:"keyword"`
	Link     float64 `json:"link"`
}

// Result is one ranked chunk.
type Result struct {
	Rank      int      `json:"rank"`
	Note      string   `json:"note"`
	Path      string   `json:"path"`
	Heading   []string `json:"heading"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Tokens    int      `json:"tokens"`
	Score     float64  `json:"score"`
	Scores    Scores   `json:"scores"`
	Excerpt   string   `json:"excerpt"`
	Text      string   `json:"text,omitempty"`
}

// BudgetInfo reports the token budget use.
type BudgetInfo struct {
	Limit     int  `json:"limit"`
	Used      int  `json:"used"`
	Truncated bool `json:"truncated"`
}

// Response is a search's answer.
type Response struct {
	Query       string      `json:"query"`
	Mode        string      `json:"mode"`
	Model       string      `json:"model"`
	KeywordOnly bool        `json:"keyword_only"`
	Results     []Result    `json:"results"`
	Budget      *BudgetInfo `json:"budget,omitempty"`
}

// semantic reports whether emb can score the index's vectors.
func semantic(ix *vindex.Index, emb embed.Embedder) bool {
	return emb != nil && ix.Meta.Dims > 0 && emb.Dims() == ix.Meta.Dims
}

// Search ranks the index's chunks for query. emb may be nil (keyword and
// link mode); mode semantic then returns ErrNoModel.
func Search(ix *vindex.Index, emb embed.Embedder, query string, o Options) (*Response, error) {
	mode := o.Mode
	if mode == "" {
		mode = ModeHybrid
	}
	if mode != ModeHybrid && mode != ModeSemantic && mode != ModeKeyword {
		return nil, errors.New("unknown mode " + mode + " (hybrid, semantic or keyword)")
	}
	useSem := mode != ModeKeyword && semantic(ix, emb)
	if mode == ModeSemantic && !useSem {
		return nil, ErrNoModel
	}
	useKw := mode != ModeSemantic
	// Zero rows are "not embedded yet": they score by keyword, whatever the mode.
	zeroRow := map[int]bool{}
	if useSem {
		for i := range ix.Chunks {
			if v := ix.Vector(i); v != nil && zero(v) {
				zeroRow[i] = true
			}
		}
	}
	k := o.K
	if k <= 0 {
		k = DefaultK
	}
	resp := &Response{Query: query, Mode: mode, Model: ix.Meta.Model, KeywordOnly: !useSem, Results: []Result{}}

	cand := candidates(ix, o)
	sem := make([]float64, len(ix.Chunks))
	if useSem {
		qv, err := emb.Embed([]string{query}, true)
		if err != nil {
			return nil, err
		}
		if len(qv) == 1 {
			for _, i := range cand {
				if v := ix.Vector(i); v != nil {
					if d := dot(qv[0], v); d > 0 {
						sem[i] = d
					}
				}
			}
		}
	}
	var kw []float64
	if useKw || len(zeroRow) > 0 {
		kw = normalise(bm25(ix, query), cand)
	} else {
		kw = make([]float64, len(ix.Chunks))
	}
	ws, wk := WeightSemantic, WeightKeyword
	switch {
	case !useSem:
		ws, wk = 0, 1
	case !useKw:
		ws, wk = 1, 0
	}
	// A zero row has no semantic score: it gets the keyword part plus the mean
	// semantic score of the embedded candidates, scaled by its keyword score, so
	// both kinds sit on one scale (W4 S03): a strong semantic hit beats a
	// mediocre keyword-only row.
	var meanSem float64
	if len(zeroRow) > 0 {
		meanSem = meanSemantic(cand, sem, func(i int) bool { return zeroRow[i] || ix.Vector(i) == nil })
	}
	base := make([]float64, len(ix.Chunks))
	for _, i := range cand {
		if zeroRow[i] {
			base[i] = zeroScore(ws, wk, meanSem, kw[i])
			// Report the imputed semantic part: score == ws*semantic + wk*keyword + link.
			sem[i] = meanSem * kw[i]
			continue
		}
		base[i] = ws*sem[i] + wk*kw[i]
	}
	order := append([]int(nil), cand...)
	rank := func() {
		sort.SliceStable(order, func(a, b int) bool {
			if base[order[a]] != base[order[b]] {
				return base[order[a]] > base[order[b]]
			}
			return order[a] < order[b]
		})
	}
	rank()

	link := make([]float64, len(ix.Chunks))
	if !o.NoLinks && len(order) > 0 {
		seeds := map[int]bool{}
		for _, i := range order {
			if base[i] <= 0 || len(seeds) >= linkSeeds {
				break
			}
			seeds[ix.Chunks[i].Note] = true
		}
		linked := linkedNotes(ix, seeds)
		for _, i := range cand {
			if n := ix.Chunks[i].Note; linked[n] && !seeds[n] && base[i] > 0 {
				link[i] = LinkBoost
			}
		}
		for _, i := range order {
			base[i] += link[i]
		}
		rank()
	}

	var used int
	for _, i := range order {
		if len(resp.Results) >= k || base[i] <= 0 {
			break
		}
		c := ix.Chunks[i]
		if o.Budget > 0 && len(resp.Results) > 0 && used+c.Tokens > o.Budget {
			resp.Budget = &BudgetInfo{Limit: o.Budget, Truncated: true}
			break
		}
		used += c.Tokens
		n := ix.Notes[c.Note]
		r := Result{
			Rank: len(resp.Results) + 1, Note: n.Name, Path: n.Path,
			Heading: c.Heading, StartLine: c.StartLine, EndLine: c.EndLine, Tokens: c.Tokens,
			Score:   base[i],
			Scores:  Scores{Semantic: sem[i], Keyword: kw[i], Link: link[i]},
			Excerpt: excerpt(c.Text),
		}
		if r.Heading == nil {
			r.Heading = []string{}
		}
		if o.Budget > 0 {
			r.Text = c.Text
		}
		resp.Results = append(resp.Results, r)
	}
	if o.Budget > 0 {
		if resp.Budget == nil {
			resp.Budget = &BudgetInfo{Limit: o.Budget}
		}
		resp.Budget.Used = used
	}
	return resp, nil
}

// candidates are the chunks that pass the filters, in row order.
func candidates(ix *vindex.Index, o Options) []int {
	ok := make([]bool, len(ix.Notes))
	for n, nm := range ix.Notes {
		ok[n] = matches(nm, o)
	}
	var out []int
	for i, c := range ix.Chunks {
		if ok[c.Note] {
			out = append(out, i)
		}
	}
	return out
}

func matches(n vindex.NoteMeta, o Options) bool {
	if len(o.Paths) > 0 {
		hit := false
		for _, p := range o.Paths {
			p = strings.Trim(strings.ReplaceAll(p, `\`, "/"), "/")
			if p == "" || p == "." || n.Path == p || strings.HasPrefix(n.Path, p+"/") {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if o.Type != "" && n.Type != o.Type {
		return false
	}
	if o.Project != "" && !contains(n.Projects, o.Project) && !strings.HasPrefix(n.Path, "projects/"+o.Project+"/") {
		return false
	}
	if o.Tag != "" && !contains(n.Tags, o.Tag) {
		return false
	}
	return true
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// byName maps a lower-cased note name to its note rows.
func byName(ix *vindex.Index) map[string][]int {
	m := map[string][]int{}
	for i, n := range ix.Notes {
		k := strings.ToLower(n.Name)
		m[k] = append(m[k], i)
	}
	return m
}

// outLinks are the notes note n links to (by bare name).
func outLinks(ix *vindex.Index, names map[string][]int, n int) []int {
	var out []int
	for _, l := range ix.Notes[n].Links {
		out = append(out, names[strings.ToLower(l)]...)
	}
	return out
}

// linkedNotes are the notes linked to or from any seed note.
func linkedNotes(ix *vindex.Index, seeds map[int]bool) map[int]bool {
	names := byName(ix)
	out := map[int]bool{}
	for n := range ix.Notes {
		for _, t := range outLinks(ix, names, n) {
			if seeds[n] {
				out[t] = true
			}
			if seeds[t] {
				out[n] = true
			}
		}
	}
	return out
}

// excerpt is text on one line, at most ExcerptMax characters.
func excerpt(text string) string {
	s := strings.Join(strings.Fields(text), " ")
	r := []rune(s)
	if len(r) <= ExcerptMax {
		return s
	}
	return strings.TrimSpace(string(r[:ExcerptMax-1])) + "…"
}

// dot is the dot product, unrolled by four. Vectors are L2-normalised, so it
// is the cosine similarity; a zero row scores 0.
func dot(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var s0, s1, s2, s3 float32
	i := 0
	for ; i+4 <= n; i += 4 {
		s0 += a[i] * b[i]
		s1 += a[i+1] * b[i+1]
		s2 += a[i+2] * b[i+2]
		s3 += a[i+3] * b[i+3]
	}
	for ; i < n; i++ {
		s0 += a[i] * b[i]
	}
	return float64(s0 + s1 + s2 + s3)
}

// zeroScore scores a chunk without a vector on the scale of embedded ones: its
// keyword part plus the mean semantic score, both scaled by its keyword match
// (no keyword match, no score). Semantic mode (wk 0) gives ws*meanSem*kw.
func zeroScore(ws, wk, meanSem, kw float64) float64 {
	return (wk + ws*meanSem) * kw
}

// meanSemantic is the mean of max(0, cosine) over the embedded candidates (those
// for which noVec is false); 0 when there are none. Search and Related both
// impute a zero row's semantic part from it, so the two sit on one scale
// (contract vault-search-core-v1 v1.4, W6 S01).
func meanSemantic(cand []int, sem []float64, noVec func(int) bool) float64 {
	m, _ := meanSemanticN(cand, sem, noVec)
	return m
}

// meanSemanticN is meanSemantic plus the number of embedded candidates.
func meanSemanticN(cand []int, sem []float64, noVec func(int) bool) (float64, int) {
	var sum float64
	var n int
	for _, i := range cand {
		if noVec(i) {
			continue
		}
		sum += math.Max(0, sem[i])
		n++
	}
	if n == 0 {
		return 0, 0
	}
	return sum / float64(n), n
}

package vsearch

import (
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/zero4573/claude-tickets/internal/embed"
	"github.com/zero4573/claude-tickets/internal/vindex"
)

// RelatedOptions are the related parameters.
type RelatedOptions struct {
	K             int // 0 → DefaultK
	IncludeLinked bool
	Chunks        bool // rank chunks instead of notes
}

// RelatedResult is one related note (or chunk with --chunks).
type RelatedResult struct {
	Rank      int      `json:"rank"`
	Note      string   `json:"note"`
	Path      string   `json:"path"`
	Score     float64  `json:"score"`
	Linked    bool     `json:"linked"`
	Heading   []string `json:"heading,omitempty"`
	StartLine int      `json:"start_line,omitempty"`
}

// RelatedResponse is related's answer.
type RelatedResponse struct {
	Note        string          `json:"note"`
	Path        string          `json:"path"`
	KeywordOnly bool            `json:"keyword_only"`
	Results     []RelatedResult `json:"results"`
}

// NoteIndex is the row of the note with the vault-relative path, or -1.
func NoteIndex(ix *vindex.Index, path string) int {
	for i, n := range ix.Notes {
		if n.Path == path {
			return i
		}
	}
	return -1
}

// Related ranks the notes (or chunks) most like note row n. With vectors and
// an embedder it compares note centroids; otherwise it runs BM25 with the
// note's name and headings as the query.
func Related(ix *vindex.Index, emb embed.Embedder, n int, o RelatedOptions) (*RelatedResponse, error) {
	if n < 0 || n >= len(ix.Notes) {
		return nil, errors.New("unknown note")
	}
	k := o.K
	if k <= 0 {
		k = DefaultK
	}
	self := ix.Notes[n]
	resp := &RelatedResponse{Note: self.Name, Path: self.Path, Results: []RelatedResult{}}

	names := byName(ix)
	linked := map[int]bool{}
	for _, t := range outLinks(ix, names, n) {
		linked[t] = true
	}
	delete(linked, n)
	skip := func(note int) bool {
		return note == n || (!o.IncludeLinked && linked[note])
	}

	// Chunk scores against the note.
	chunkScore := make([]float64, len(ix.Chunks))
	cen := centroid(ix, n)
	useSem := semantic(ix, emb) && cen != nil
	resp.KeywordOnly = !useSem
	q := self.Name
	for _, c := range ix.Chunks[self.First : self.First+self.Count] {
		q += " " + strings.Join(c.Heading, " ")
	}
	// noVec marks chunks with no embedding (zero rows): "unknown", so they are
	// ranked by BM25 instead of scoring as dissimilar (W3 S02).
	noVec := map[int]bool{}
	if useSem {
		for i := range ix.Chunks {
			if v := ix.Vector(i); v == nil || zero(v) {
				noVec[i] = true
			} else {
				chunkScore[i] = dot(cen, v)
			}
		}
		if len(noVec) > 0 {
			// One scale with the cosines (W4 C02): the normalised BM25 is scaled by
			// the mean cosine of the embedded chunks, so an unembedded chunk can't
			// outrank a clearly similar embedded one.
			// Related has no candidate set like Search: every unskipped chunk is
			// ranked, so the mean is over all of them (W6 S01, intentional).
			var mean float64
			var cnt int
			for i, s := range chunkScore {
				if !noVec[i] && s > 0 && !skip(ix.Chunks[i].Note) {
					mean += s
					cnt++
				}
			}
			if cnt > 0 {
				mean /= float64(cnt)
			} else {
				// Nothing embedded to compare with: rank by BM25 alone, as in
				// keyword-only mode (W6 S02, intentional; Search instead gives such
				// zero rows their keyword part only, as meanSem is 0 there).
				mean = 1
			}
			kw := normalise(bm25(ix, q), nil)
			for i := range noVec {
				chunkScore[i] = kw[i] * mean
			}
		}
	} else {
		chunkScore = normalise(bm25(ix, q), nil)
	}

	type item struct {
		chunk, note int
		score       float64
	}
	var items []item
	if o.Chunks {
		for i, c := range ix.Chunks {
			if !skip(c.Note) && chunkScore[i] > 0 {
				items = append(items, item{i, c.Note, chunkScore[i]})
			}
		}
	} else if useSem {
		for m := range ix.Notes {
			if skip(m) {
				continue
			}
			if c := centroid(ix, m); c != nil {
				if s := dot(cen, c); s > 0 {
					items = append(items, item{-1, m, s})
				}
			} else {
				// No centroid: all rows unembedded, rank by BM25.
				best := -1
				for i := ix.Notes[m].First; i < ix.Notes[m].First+ix.Notes[m].Count; i++ {
					if chunkScore[i] > 0 && (best < 0 || chunkScore[i] > chunkScore[best]) {
						best = i
					}
				}
				if best >= 0 {
					items = append(items, item{-1, m, chunkScore[best]})
				}
			}
		}
	} else {
		best := map[int]int{}
		for i, c := range ix.Chunks {
			if skip(c.Note) || chunkScore[i] <= 0 {
				continue
			}
			if b, ok := best[c.Note]; !ok || chunkScore[i] > chunkScore[b] {
				best[c.Note] = i
			}
		}
		for m, i := range best {
			items = append(items, item{-1, m, chunkScore[i]})
		}
	}
	sort.Slice(items, func(a, b int) bool {
		if items[a].score != items[b].score {
			return items[a].score > items[b].score
		}
		if items[a].note != items[b].note {
			return items[a].note < items[b].note
		}
		return items[a].chunk < items[b].chunk
	})
	for _, it := range items {
		if len(resp.Results) >= k {
			break
		}
		nm := ix.Notes[it.note]
		r := RelatedResult{Rank: len(resp.Results) + 1, Note: nm.Name, Path: nm.Path, Score: it.score, Linked: linked[it.note]}
		if it.chunk >= 0 {
			r.Heading = ix.Chunks[it.chunk].Heading
			r.StartLine = ix.Chunks[it.chunk].StartLine
		}
		resp.Results = append(resp.Results, r)
	}
	return resp, nil
}

// centroid is the normalised mean of note n's non-zero chunk vectors; nil
// when it has none.
func centroid(ix *vindex.Index, n int) []float32 {
	d := ix.Meta.Dims
	if d == 0 {
		return nil
	}
	nm := ix.Notes[n]
	sum := make([]float32, d)
	cnt := 0
	for i := nm.First; i < nm.First+nm.Count; i++ {
		v := ix.Vector(i)
		if v == nil || zero(v) {
			continue
		}
		for j, x := range v {
			sum[j] += x
		}
		cnt++
	}
	if cnt == 0 {
		return nil
	}
	var sq float64
	for _, x := range sum {
		sq += float64(x) * float64(x)
	}
	if sq == 0 {
		return nil
	}
	inv := float32(1 / math.Sqrt(sq))
	for j := range sum {
		sum[j] *= inv
	}
	return sum
}

func zero(v []float32) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}

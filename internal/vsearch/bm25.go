package vsearch

import (
	"math"
	"strings"
	"unicode"

	"github.com/zero4573/tix/internal/vindex"
)

// BM25 parameters.
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// tokenize lower-cases s and splits it into letter/digit runs, dropping
// one-character tokens.
func tokenize(s string) []string {
	var out []string
	eachToken(s, func(t string) { out = append(out, t) })
	return out
}

func eachToken(s string, fn func(string)) {
	var b strings.Builder
	n := 0
	flush := func() {
		if n > 1 {
			fn(b.String())
		}
		b.Reset()
		n = 0
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			n++
		} else {
			flush()
		}
	}
	flush()
}

// chunkDoc is what BM25 sees of a chunk: its text, heading path and note name.
func chunkDoc(ix *vindex.Index, c vindex.ChunkMeta) string {
	return c.Text + "\n" + strings.Join(c.Heading, " ") + "\n" + ix.Notes[c.Note].Name
}

// bm25 scores every chunk of the index against the query terms; the corpus
// statistics come from all chunks. The result has one score per chunk.
func bm25(ix *vindex.Index, query string) []float64 {
	scores := make([]float64, len(ix.Chunks))
	terms := map[string]int{}
	for _, t := range tokenize(query) {
		if _, ok := terms[t]; !ok {
			terms[t] = len(terms)
		}
	}
	if len(terms) == 0 || len(ix.Chunks) == 0 {
		return scores
	}
	nt := len(terms)
	tf := make([]int32, len(ix.Chunks)*nt)
	dl := make([]int, len(ix.Chunks))
	df := make([]int, nt)
	total := 0
	for i, c := range ix.Chunks {
		n := 0
		eachToken(chunkDoc(ix, c), func(t string) {
			n++
			if k, ok := terms[t]; ok {
				tf[i*nt+k]++
			}
		})
		dl[i] = n
		total += n
		for k := 0; k < nt; k++ {
			if tf[i*nt+k] > 0 {
				df[k]++
			}
		}
	}
	avg := float64(total) / float64(len(ix.Chunks))
	if avg == 0 {
		avg = 1
	}
	N := float64(len(ix.Chunks))
	idf := make([]float64, nt)
	for k := range idf {
		idf[k] = math.Log(1 + (N-float64(df[k])+0.5)/(float64(df[k])+0.5))
	}
	for i := range ix.Chunks {
		var s float64
		for k := 0; k < nt; k++ {
			f := float64(tf[i*nt+k])
			if f == 0 {
				continue
			}
			s += idf[k] * f * (bm25K1 + 1) / (f + bm25K1*(1-bm25B+bm25B*float64(dl[i])/avg))
		}
		scores[i] = s
	}
	return scores
}

// normalise scales xs to [0,1] by min-max over the positions in idx (all
// when idx is nil), with 0 as the floor so the weakest match still counts.
func normalise(xs []float64, idx []int) []float64 {
	out := make([]float64, len(xs))
	lo, hi := 0.0, 0.0 // scores are >= 0; a zero floor keeps the weakest match above 0
	visit := func(fn func(i int)) {
		if idx == nil {
			for i := range xs {
				fn(i)
			}
			return
		}
		for _, i := range idx {
			fn(i)
		}
	}
	visit(func(i int) { lo = math.Min(lo, xs[i]); hi = math.Max(hi, xs[i]) })
	visit(func(i int) {
		switch {
		case hi > lo:
			out[i] = (xs[i] - lo) / (hi - lo)
		case xs[i] > 0:
			out[i] = 1
		}
	})
	return out
}

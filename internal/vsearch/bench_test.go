package vsearch

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/zero4573/tix/internal/embed"
	"github.com/zero4573/tix/internal/vindex"
)

// BenchmarkSearch20k is a top-10 hybrid search over 20,000 synthetic 384-dim
// chunks, including vindex.Load (target: under 1 s per op).
func BenchmarkSearch20k(b *testing.B) {
	const notes, per, dims = 2000, 10, 384
	v := b.TempDir()
	dir := vindex.Dir(v)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.Fatal(err)
	}
	rng := rand.New(rand.NewSource(1))
	words := make([]string, 3000)
	for i := range words {
		words[i] = fmt.Sprintf("w%d", i)
	}
	text := func() string {
		s := ""
		for i := 0; i < 40; i++ {
			s += words[rng.Intn(len(words))] + " "
		}
		return s
	}
	var nrows, crows []byte
	vecs := make([]byte, 0, notes*per*dims*4)
	for n := 0; n < notes; n++ {
		nb, _ := json.Marshal(vindex.NoteMeta{Path: fmt.Sprintf("n/%d.md", n), Name: fmt.Sprintf("n%d", n),
			Links: []string{fmt.Sprintf("n%d", (n+1)%notes)}, First: n * per, Count: per})
		nrows = append(append(nrows, nb...), '\n')
		for c := 0; c < per; c++ {
			cb, _ := json.Marshal(vindex.ChunkMeta{Note: n, Heading: []string{"H"}, StartLine: 1, EndLine: 5, Tokens: 60, Hash: "h", Text: text()})
			crows = append(append(crows, cb...), '\n')
			var sq float64
			vec := make([]float32, dims)
			for j := range vec {
				vec[j] = float32(rng.NormFloat64())
				sq += float64(vec[j]) * float64(vec[j])
			}
			for _, x := range vec {
				vecs = binary.LittleEndian.AppendUint32(vecs, math.Float32bits(x/float32(math.Sqrt(sq))))
			}
		}
	}
	mb, _ := json.Marshal(vindex.Meta{Format: vindex.Format, Model: "stub", Dims: dims, ChunkTokens: 256, Notes: notes, Chunks: notes * per})
	for name, data := range map[string][]byte{"meta.json": mb, "notes.jsonl": nrows, "chunks.jsonl": crows, "vectors.f32": vecs} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			b.Fatal(err)
		}
	}
	emb := embed.Stub(dims)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ix, err := vindex.Load(v)
		if err != nil {
			b.Fatal(err)
		}
		r, err := Search(ix, emb, "w12 w99 w1500 something", Options{K: 10})
		if err != nil || len(r.Results) == 0 {
			b.Fatal(err, len(r.Results))
		}
	}
}

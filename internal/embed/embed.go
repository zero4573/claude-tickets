// Package embed turns text into L2-normalised vectors: a local llama.cpp
// model through purego (no cgo) and a deterministic stub for tests.
package embed

import (
	"errors"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// ErrUnavailable means the native library or the model is missing, or the
// platform can't load it; callers fall back to keyword-only search.
var ErrUnavailable = errors.New("embedding unavailable")

// Embedder produces embeddings.
type Embedder interface {
	// ID is the model id, e.g. "bge-small-en-v1.5"; the stub's is "stub".
	ID() string
	Dims() int
	// Embed returns one L2-normalised vector per text; query=true adds the
	// model's query prefix.
	Embed(texts []string, query bool) ([][]float32, error)
	Close() error
}

// LlamaOptions configures OpenLlama.
type LlamaOptions struct {
	ID, QueryPrefix string
	Threads, Ctx    int // Ctx 0 -> 512, Threads 0 -> up to 4
}

// DefaultLibPath is a list of extra directories (os.PathListSeparator
// separated) searched for the native dependencies of the llama.cpp library
// (libstdc++, libgomp), after $CLAUDE_TICKETS_LIB_PATH. The Nix package sets
// it with -ldflags -X.
var DefaultLibPath string

// Stub is a deterministic hashed bag-of-words embedder for tests: the same
// text gives the same vector, and texts sharing words have a higher cosine.
func Stub(dims int) Embedder {
	if dims <= 0 {
		dims = 64
	}
	return stub{dims}
}

type stub struct{ dims int }

func (s stub) ID() string   { return "stub" }
func (s stub) Dims() int    { return s.dims }
func (s stub) Close() error { return nil }

func (s stub) Embed(texts []string, query bool) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, s.dims)
		words := strings.FieldsFunc(strings.ToLower(t), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
		for _, w := range words {
			h := fnv.New64a()
			h.Write([]byte(w))
			sum := h.Sum64()
			idx := int(sum % uint64(s.dims))
			if sum>>63 == 1 {
				v[idx]--
			} else {
				v[idx]++
			}
		}
		normalize(v)
		out[i] = v
	}
	return out, nil
}

// normalize scales v to unit length (a zero vector stays zero).
func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}

// Cosine is the dot product of two L2-normalised vectors.
func Cosine(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

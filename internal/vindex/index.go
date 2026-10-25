// Package vindex is the vault search index under <vault>/.ct/index: notes,
// chunks and (when an embedding model is available) one vector per chunk.
// The format is the vault-search-core-v1 contract's "On-disk index".
package vindex

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/zero4573/claude-tickets/internal/lock"
)

// Format is meta.json's "format".
const Format = 1

// ErrNoIndex means there is no usable index: none was built, or its files
// are damaged or out of step with meta.json (the next Update rebuilds it).
var ErrNoIndex = errors.New("no vault index")

// Dir is where the index files are.
func Dir(vault string) string { return filepath.Join(vault, ".ct", "index") }

// Meta is meta.json.
type Meta struct {
	Format      int    `json:"format"`
	Model       string `json:"model"`
	ModelSHA256 string `json:"model_sha256"`
	Dims        int    `json:"dims"`
	ChunkTokens int    `json:"chunk_tokens"`
	Notes       int    `json:"notes"`
	Chunks      int    `json:"chunks"`
	Updated     string `json:"updated"`
}

// NoteMeta is one line of notes.jsonl.
type NoteMeta struct {
	Path     string   `json:"path"`
	Name     string   `json:"name"`
	MTime    int64    `json:"mtime"` // unix ns
	Size     int64    `json:"size"`
	SHA256   string   `json:"sha256"`
	Type     string   `json:"type"`
	Tags     []string `json:"tags"`
	Projects []string `json:"projects"`
	Links    []string `json:"links"`
	First    int      `json:"first"` // row of the note's first chunk
	Count    int      `json:"count"`
}

// ChunkMeta is one line of chunks.jsonl.
type ChunkMeta struct {
	Note      int      `json:"note"` // notes.jsonl line index
	Heading   []string `json:"heading"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Tokens    int      `json:"tokens"`
	Hash      string   `json:"hash"`
	Text      string   `json:"text"`
}

// Index is a loaded index.
type Index struct {
	Meta   Meta
	Notes  []NoteMeta
	Chunks []ChunkMeta
	vecs   []float32
}

// Vector is chunk i's L2-normalised vector; nil when the index is keyword-only.
func (ix *Index) Vector(i int) []float32 {
	d := ix.Meta.Dims
	if d == 0 || i < 0 || (i+1)*d > len(ix.vecs) {
		return nil
	}
	return ix.vecs[i*d : (i+1)*d]
}

// Load reads the index under index.lock, so it never sees a set a writer is
// half through. A missing, damaged or inconsistent index gives an error
// wrapping ErrNoIndex. Where the lock file can't be opened (a read-only
// vault) it reads without it.
func Load(vault string) (*Index, error) {
	if _, err := os.Stat(filepath.Join(Dir(vault), "meta.json")); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoIndex, err)
	}
	var ix *Index
	var lerr error
	ran := false
	if err := lock.With(filepath.Join(vault, ".ct", "index.lock"), func() error {
		ran = true
		ix, lerr = load(vault)
		return nil
	}); err != nil && !ran {
		return load(vault)
	}
	return ix, lerr
}

// load is Load for a caller holding the lock.
func load(vault string) (*Index, error) {
	dir := Dir(vault)
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoIndex, err)
	}
	ix := &Index{}
	if err := json.Unmarshal(data, &ix.Meta); err != nil {
		return nil, fmt.Errorf("%w: meta.json: %v", ErrNoIndex, err)
	}
	if ix.Meta.Format != Format {
		return nil, fmt.Errorf("%w: format %d", ErrNoIndex, ix.Meta.Format)
	}
	if err := readLines(filepath.Join(dir, "notes.jsonl"), func(b []byte) error {
		var n NoteMeta
		if err := json.Unmarshal(b, &n); err != nil {
			return err
		}
		ix.Notes = append(ix.Notes, n)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("%w: notes.jsonl: %v", ErrNoIndex, err)
	}
	if err := readLines(filepath.Join(dir, "chunks.jsonl"), func(b []byte) error {
		var c ChunkMeta
		if err := json.Unmarshal(b, &c); err != nil {
			return err
		}
		ix.Chunks = append(ix.Chunks, c)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("%w: chunks.jsonl: %v", ErrNoIndex, err)
	}
	if len(ix.Notes) != ix.Meta.Notes || len(ix.Chunks) != ix.Meta.Chunks {
		return nil, fmt.Errorf("%w: files don't match meta.json", ErrNoIndex)
	}
	// Notes tile the chunk rows: note i owns rows [First, First+Count), in order.
	next := 0
	for i, n := range ix.Notes {
		if n.First != next || n.Count < 0 || n.First+n.Count > len(ix.Chunks) {
			return nil, fmt.Errorf("%w: note %q doesn't tile the chunks", ErrNoIndex, n.Path)
		}
		for r := n.First; r < n.First+n.Count; r++ {
			if ix.Chunks[r].Note != i {
				return nil, fmt.Errorf("%w: chunk %d is not note %d's", ErrNoIndex, r, i)
			}
		}
		next += n.Count
	}
	if next != len(ix.Chunks) {
		return nil, fmt.Errorf("%w: chunks without a note", ErrNoIndex)
	}
	if ix.Meta.Dims > 0 {
		raw, err := os.ReadFile(filepath.Join(dir, "vectors.f32"))
		if err != nil || len(raw) != ix.Meta.Chunks*ix.Meta.Dims*4 {
			return nil, fmt.Errorf("%w: vectors.f32 doesn't match meta.json", ErrNoIndex)
		}
		ix.vecs = make([]float32, len(raw)/4)
		for i := range ix.vecs {
			ix.vecs[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}
	}
	return ix, nil
}

func readLines(path string, fn func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if t := bytes.TrimSpace(line); len(t) > 0 {
			if e := fn(t); e != nil {
				return e
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// writeAtomic writes path through a temp file in its directory and a rename.
func writeAtomic(path string, fn func(w io.Writer) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	w := bufio.NewWriterSize(tmp, 1<<20)
	err = fn(w)
	if err == nil {
		err = w.Flush()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func writeJSONL[T any](path string, rows []T) error {
	return writeAtomic(path, func(w io.Writer) error {
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		for _, r := range rows {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
		return nil
	})
}

func writeVectors(path string, rows [][]float32) error {
	return writeAtomic(path, func(w io.Writer) error {
		var b [4]byte
		for _, r := range rows {
			for _, f := range r {
				binary.LittleEndian.PutUint32(b[:], math.Float32bits(f))
				if _, err := w.Write(b[:]); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

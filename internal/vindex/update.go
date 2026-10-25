package vindex

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zero4573/claude-tickets/internal/chunk"
	"github.com/zero4573/claude-tickets/internal/embed"
	"github.com/zero4573/claude-tickets/internal/links"
	"github.com/zero4573/claude-tickets/internal/lock"
	"github.com/zero4573/claude-tickets/internal/modelcache"
	"github.com/zero4573/claude-tickets/internal/note"
)

// ModelHint is printed (stderr, once per index) when there is no embedder.
const ModelHint = "ct: no embedding model, keyword search only; on the host run: ct vault model fetch"

const batchSize = 32

// UpdateOptions tunes Update.
type UpdateOptions struct {
	Full        bool
	Model       string    // "" → the index's model, else modelcache.Default().ID
	ChunkTokens int       // 0 → the index's, else chunk.DefaultMaxTokens
	Files       []string  // vault-relative: only check these (Refresh); nil = whole vault
	Log         io.Writer // progress; nil = quiet
}

// UpdateStats is what an Update did.
type UpdateStats struct {
	Notes, Changed, Removed, Chunks, Embedded int
	Model                                     string
	KeywordOnly                               bool
	Took                                      time.Duration
}

// skippedTop are the vault's top-level folders that are not indexed.
var skippedTop = map[string]bool{"raw": true, "archive": true, "templates": true}

// indexable says whether a vault-relative, "/"-separated path is indexed.
func indexable(rel string) bool {
	if !strings.HasSuffix(rel, ".md") {
		return false
	}
	parts := strings.Split(rel, "/")
	for _, p := range parts {
		if strings.HasPrefix(p, ".") {
			return false
		}
	}
	return len(parts) == 1 || !skippedTop[parts[0]]
}

func walk(vault string) []string {
	var out []string
	_ = filepath.WalkDir(vault, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(vault, p)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || (filepath.Dir(rel) == "." && skippedTop[d.Name()]) {
				return filepath.SkipDir
			}
			return nil
		}
		if r := filepath.ToSlash(rel); d.Type().IsRegular() && indexable(r) {
			out = append(out, r)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// built is a note of the index being written, with its chunks and vectors.
type built struct {
	meta   NoteMeta
	chunks []ChunkMeta
	vecs   [][]float32
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// embedInput is the text a chunk is embedded (and hashed) as.
func embedInput(name string, c chunk.Chunk) string {
	return name + " > " + strings.Join(c.Heading, " > ") + "\n\n" + cleanLinks(c.Text)
}

// Update brings the index in line with the vault: new and changed notes are
// chunked and embedded (vectors are reused by chunk hash), removed notes
// dropped. A note is unchanged when mtime and size match, or the sha256 does.
// A different model, model checksum or chunk size rebuilds everything. With no
// embedding model (embed.ErrUnavailable) the index is keyword-only.
func Update(vault string, o UpdateOptions) (UpdateStats, error) {
	start := time.Now()
	var st UpdateStats
	ct := filepath.Join(vault, ".ct")
	if err := os.MkdirAll(Dir(vault), 0o755); err != nil {
		return st, err
	}
	if _, err := os.Stat(filepath.Join(ct, ".gitignore")); err != nil {
		if err := os.WriteFile(filepath.Join(ct, ".gitignore"), []byte("*\n"), 0o644); err != nil {
			return st, err
		}
	}
	err := lock.With(filepath.Join(ct, "index.lock"), func() error { return update(vault, o, &st) })
	st.Took = time.Since(start)
	return st, err
}

func update(vault string, o UpdateOptions, st *UpdateStats) error {
	logf := func(f string, a ...any) {
		if o.Log != nil {
			fmt.Fprintf(o.Log, f+"\n", a...)
		}
	}
	old, oerr := Load(vault)
	if oerr != nil {
		old = nil
		if !errors.Is(oerr, ErrNoIndex) {
			return oerr
		}
	}
	// Model: requested, else the index's, else the default.
	want := o.Model
	if want == "" && old != nil {
		want = old.Meta.Model
	}
	if _, ok := modelcache.Lookup(want); !ok && o.Model == "" {
		want = modelcache.Default().ID
	}
	chunkTokens := o.ChunkTokens
	if chunkTokens <= 0 && old != nil {
		chunkTokens = old.Meta.ChunkTokens
	}
	if chunkTokens <= 0 {
		chunkTokens = chunk.DefaultMaxTokens
	}

	var emb embed.Embedder
	e, err := modelcache.Open(want)
	switch {
	case err == nil:
		emb = e
		defer e.Close()
	case errors.Is(err, embed.ErrUnavailable):
	default:
		return err
	}
	meta := Meta{Format: Format, ChunkTokens: chunkTokens}
	if emb != nil {
		meta.Model, meta.Dims = emb.ID(), emb.Dims()
		if m, ok := modelcache.Lookup(want); ok && emb.ID() == m.ID {
			meta.ModelSHA256 = m.SHA256
		}
	}
	st.Model, st.KeywordOnly = meta.Model, emb == nil

	full := o.Full || old == nil || old.Meta.Model != meta.Model || old.Meta.ModelSHA256 != meta.ModelSHA256 ||
		old.Meta.Dims != meta.Dims || old.Meta.ChunkTokens != chunkTokens
	if full {
		old = nil
	}

	// Which paths to look at, and which old notes carry over untouched.
	var paths []string
	keep := map[string]int{} // old notes outside the checked set
	oldByPath := map[string]int{}
	if old != nil {
		for i, n := range old.Notes {
			oldByPath[n.Path] = i
		}
	}
	if old != nil && o.Files != nil {
		set := map[string]bool{}
		for _, f := range o.Files {
			f = filepath.ToSlash(f)
			if filepath.IsAbs(f) {
				if r, err := filepath.Rel(vault, filepath.FromSlash(f)); err == nil {
					f = filepath.ToSlash(r)
				}
			}
			if !set[f] && indexable(f) {
				set[f] = true
				paths = append(paths, f)
			}
		}
		for p, i := range oldByPath {
			if !set[p] {
				keep[p] = i
			}
		}
	} else {
		paths = walk(vault)
	}

	// Reusable vectors by chunk hash.
	reuse := map[string][]float32{}
	if old != nil && old.Meta.Dims > 0 {
		for i, c := range old.Chunks {
			reuse[c.Hash] = old.Vector(i)
		}
	}

	var notes []*built
	rewriteNotes := false // some mtime/size changed
	oldChunksOf := func(i int) *built {
		n := old.Notes[i]
		b := &built{meta: n}
		for r := n.First; r < n.First+n.Count; r++ {
			b.chunks = append(b.chunks, old.Chunks[r])
			if old.Meta.Dims > 0 {
				b.vecs = append(b.vecs, old.Vector(r))
			}
		}
		return b
	}
	for p, i := range keep {
		_ = p
		notes = append(notes, oldChunksOf(i))
	}
	type pending struct {
		b      *built
		inputs []string
		idx    []int // rows (in b.chunks) needing a vector
	}
	var todo []pending
	exists := map[string]bool{}
	for _, rel := range paths {
		abs := filepath.Join(vault, filepath.FromSlash(rel))
		fi, err := os.Stat(abs)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		exists[rel] = true
		oi, had := oldByPath[rel]
		if had && old.Notes[oi].MTime == fi.ModTime().UnixNano() && old.Notes[oi].Size == fi.Size() {
			notes = append(notes, oldChunksOf(oi))
			continue
		}
		src, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		h := sum(src)
		if had && old.Notes[oi].SHA256 == h {
			b := oldChunksOf(oi)
			b.meta.MTime, b.meta.Size = fi.ModTime().UnixNano(), fi.Size()
			notes = append(notes, b)
			rewriteNotes = true
			continue
		}
		st.Changed++
		name := strings.TrimSuffix(filepath.Base(rel), ".md")
		nm := NoteMeta{
			Path: rel, Name: name, MTime: fi.ModTime().UnixNano(), Size: fi.Size(), SHA256: h,
			Type:     note.Frontmatter(abs)["type"],
			Tags:     orEmpty(note.ListField(abs, "tags")),
			Projects: orEmpty(note.ListField(abs, "projects")),
		}
		seen := map[string]bool{}
		add := func(t string) {
			if t = links.BareName(t); t != "" && !seen[t] {
				seen[t] = true
				nm.Links = append(nm.Links, t)
			}
		}
		for _, t := range links.Targets(string(src)) {
			add(t)
		}
		for _, f := range []string{"depends-on", "groups"} {
			for _, t := range note.ListField(abs, f) {
				add(unwrap(t))
			}
		}
		nm.Links = orEmpty(nm.Links)
		b := &built{meta: nm}
		p := pending{b: b}
		for _, c := range chunk.Split(src, chunk.Options{MaxTokens: chunkTokens}) {
			in := embedInput(name, c)
			cm := ChunkMeta{
				Heading: orEmpty(c.Heading), StartLine: c.StartLine, EndLine: c.EndLine,
				Tokens: c.Tokens, Hash: sum([]byte(in))[:16], Text: cleanLinks(c.Text),
			}
			b.chunks = append(b.chunks, cm)
			if emb != nil {
				if v, ok := reuse[cm.Hash]; ok && v != nil {
					b.vecs = append(b.vecs, v)
				} else {
					b.vecs = append(b.vecs, nil)
					p.inputs = append(p.inputs, in)
					p.idx = append(p.idx, len(b.vecs)-1)
				}
			}
		}
		todo = append(todo, p)
		notes = append(notes, b)
	}
	// Old notes in the checked set that are gone.
	for p := range oldByPath {
		if _, k := keep[p]; !k && !exists[p] {
			st.Removed++
		}
	}

	// Embed what's new, in batches across notes.
	if emb != nil {
		var inputs []string
		var dest []*[]float32
		for _, p := range todo {
			for k, row := range p.idx {
				inputs = append(inputs, p.inputs[k])
				dest = append(dest, &p.b.vecs[row])
			}
		}
		if len(inputs) > 0 {
			logf("embedding %d chunks", len(inputs))
		}
		for i := 0; i < len(inputs); i += batchSize {
			j := min(i+batchSize, len(inputs))
			vs, err := emb.Embed(inputs[i:j], false)
			if err != nil {
				return fmt.Errorf("embedding: %w", err)
			}
			if len(vs) != j-i {
				return fmt.Errorf("embedding: got %d vectors for %d texts", len(vs), j-i)
			}
			for k, v := range vs {
				*dest[i+k] = v
			}
			st.Embedded += len(vs)
		}
	}

	// Assemble, numbering notes and chunks.
	sort.Slice(notes, func(i, j int) bool { return notes[i].meta.Path < notes[j].meta.Path })
	var noteRows []NoteMeta
	var chunkRows []ChunkMeta
	var vecRows [][]float32
	for ni, b := range notes {
		b.meta.First, b.meta.Count = len(chunkRows), len(b.chunks)
		for ci, c := range b.chunks {
			c.Note = ni
			chunkRows = append(chunkRows, c)
			if emb != nil {
				vecRows = append(vecRows, b.vecs[ci])
			}
		}
		noteRows = append(noteRows, b.meta)
	}
	st.Notes, st.Chunks = len(noteRows), len(chunkRows)
	meta.Notes, meta.Chunks = st.Notes, st.Chunks

	dir := Dir(vault)
	unchanged := old != nil && st.Changed == 0 && st.Removed == 0 && !rewriteNotes
	if !unchanged {
		structural := full || old == nil || st.Changed > 0 || st.Removed > 0
		if structural {
			if err := writeJSONL(filepath.Join(dir, "chunks.jsonl"), chunkRows); err != nil {
				return err
			}
			if emb != nil {
				if err := writeVectors(filepath.Join(dir, "vectors.f32"), vecRows); err != nil {
					return err
				}
			} else if err := os.Remove(filepath.Join(dir, "vectors.f32")); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err := writeJSONL(filepath.Join(dir, "notes.jsonl"), noteRows); err != nil {
			return err
		}
		meta.Updated = time.Now().UTC().Format(time.RFC3339)
		if err := writeMeta(filepath.Join(dir, "meta.json"), meta); err != nil {
			return err
		}
	}

	hint := filepath.Join(dir, "model-hint")
	if emb != nil {
		os.Remove(hint)
	} else if _, err := os.Stat(hint); err != nil {
		fmt.Fprintln(os.Stderr, ModelHint)
		_ = os.WriteFile(hint, nil, 0o644)
	}
	return nil
}

func writeMeta(path string, m Meta) error {
	return writeAtomic(path, func(w io.Writer) error {
		b, err := jsonMarshal(m)
		if err != nil {
			return err
		}
		_, err = w.Write(append(b, '\n'))
		return err
	})
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// unwrap turns a frontmatter list item such as "[[a|b]]" into its target.
func unwrap(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[[") && strings.HasSuffix(s, "]]") {
		s = s[2 : len(s)-2]
	}
	if i := strings.IndexAny(s, "|#"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSuffix(strings.TrimSpace(s), `\`)
}

// Refresh updates the index for files changed by a ct write (vault-relative
// paths). With no index it does nothing; errors go to stderr, never to the caller.
func Refresh(vault string, files ...string) {
	if _, err := os.Stat(filepath.Join(Dir(vault), "meta.json")); err != nil {
		return
	}
	if len(files) == 0 {
		return
	}
	if _, err := Update(vault, UpdateOptions{Files: files}); err != nil {
		fmt.Fprintf(os.Stderr, "ct: vault index not updated: %v\n", err)
	}
}

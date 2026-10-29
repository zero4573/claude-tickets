package vindex

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zero4573/claude-tickets/internal/gitx"
)

// Describer is an optional Source method: it names a document, which
// otherwise gets its file's base name, the type of its frontmatter and its
// frontmatter's projects. Spec files are all called spec.md, so their
// source names them from the capability and the repo.
type Describer interface {
	Describe(rel string) (name, typ string, projects []string)
}

// SpecType is the type of a spec document, in the index and in search.
const SpecType = "spec"

// SpecIndexRoot is where a repo's spec index lives under the cache.
func SpecIndexRoot(cache, slug string) string { return filepath.Join(cache, "spec-index", slug) }

type specBlob struct {
	oid  string
	size int64
}

// gitSpecSource is a repo's specs/<capability>/spec.md files as of one
// commit, read with git, not from the working tree.
type gitSpecSource struct {
	clone, ref, slug string
	blobs            map[string]specBlob
}

// SpecRef is the ref a clone's specs are read from: its default branch
// (origin/HEAD), else the branch its HEAD is on. "" when it has none.
func SpecRef(clone string) string {
	if r, err := gitx.Out(clone, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && r != "" && gitx.HasRef(clone, r+"^{commit}") {
		return r
	}
	if r, err := gitx.Out(clone, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && r != "" && r != "HEAD" {
		return r
	}
	return ""
}

// SpecTree is the hash of ref's specs/ tree; ok is false when the ref has
// none.
func SpecTree(clone, ref string) (string, bool) {
	h, err := gitx.Out(clone, "rev-parse", "--verify", "-q", ref+":specs")
	return h, err == nil && h != ""
}

func isSpecPath(rel string) bool {
	p := strings.Split(rel, "/")
	return len(p) == 3 && p[0] == "specs" && p[2] == "spec.md" && !strings.HasPrefix(p[1], ".")
}

// GitSpecSource is the Source of the spec files of ref in clone: paths are
// specs/<capability>/spec.md. Documents are told apart by blob id.
func GitSpecSource(clone, ref, slug string) (Source, error) {
	s := &gitSpecSource{clone: clone, ref: ref, slug: slug, blobs: map[string]specBlob{}}
	out, err := gitx.Out(clone, "ls-tree", "-r", "-l", "-z", "--full-tree", ref, "--", "specs")
	if err != nil {
		return nil, err
	}
	for _, e := range strings.Split(out, "\x00") {
		meta, name, ok := strings.Cut(e, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 4 || f[1] != "blob" || !isSpecPath(name) {
			continue
		}
		size, _ := strconv.ParseInt(f[3], 10, 64)
		s.blobs[name] = specBlob{oid: f[2], size: size}
	}
	return s, nil
}

func (s *gitSpecSource) List() ([]string, error) {
	out := make([]string, 0, len(s.blobs))
	for p := range s.blobs {
		out = append(out, p)
	}
	return out, nil
}

func (s *gitSpecSource) Normalize(p string) (string, bool) {
	p = filepath.ToSlash(p)
	return p, isSpecPath(p)
}

func (s *gitSpecSource) Stat(rel string) (FileInfo, error) {
	b, ok := s.blobs[rel]
	if !ok {
		return FileInfo{}, fs.ErrNotExist
	}
	// No file times in a tree: the blob id's first 63 bits stand in, so an
	// edit that keeps the size still reads as changed.
	raw, _ := hex.DecodeString(b.oid[:16])
	var m int64
	for _, c := range raw {
		m = m<<8 | int64(c)
	}
	return FileInfo{MTime: m&(1<<62-1) + 1, Size: b.size}, nil
}

func (s *gitSpecSource) Read(rel string) ([]byte, error) {
	b, ok := s.blobs[rel]
	if !ok {
		return nil, fs.ErrNotExist
	}
	out, err := gitx.Out(s.clone, "cat-file", "blob", b.oid)
	if err != nil {
		return nil, err
	}
	// Out trims trailing newlines; restore the one every text file ends with
	// so the bytes (and their hash) are the file's, up to blank lines at the end.
	return []byte(out + "\n"), nil
}

// Describe names a spec after its capability, typed spec and of the repo's
// slug.
func (s *gitSpecSource) Describe(rel string) (string, string, []string) {
	return path.Base(path.Dir(rel)), SpecType, []string{s.slug}
}

const treeFile = "specs.tree"

// UpdateSpecs brings the spec index of clone (repo slug) under the cache up
// to date with the specs/ tree of its default branch. The tree hash of the
// last build is kept next to the index, so an unchanged tree costs one git
// call. built says whether there is an index to search; a repo without
// specs on its default branch has none (a stale one is removed).
func UpdateSpecs(cache, slug, clone string, o UpdateOptions) (root string, built bool, err error) {
	root = SpecIndexRoot(cache, slug)
	ref := SpecRef(clone)
	tree, ok := "", false
	if ref != "" {
		tree, ok = SpecTree(clone, ref)
	}
	if !ok {
		if err := os.RemoveAll(root); err != nil {
			return root, false, err
		}
		return root, false, nil
	}
	if b, rerr := os.ReadFile(filepath.Join(root, treeFile)); rerr == nil && strings.TrimSpace(string(b)) == tree && !o.Full {
		if _, lerr := Load(root); lerr == nil {
			return root, true, nil
		}
	}
	src, err := GitSpecSource(clone, ref, slug)
	if err != nil {
		return root, false, fmt.Errorf("specs of %s: %w", slug, err)
	}
	if _, err := UpdateSource(root, src, o); err != nil {
		return root, false, err
	}
	if err := os.WriteFile(filepath.Join(root, treeFile), []byte(tree+"\n"), 0o644); err != nil {
		return root, false, err
	}
	return root, true, nil
}

// LoadSpecs loads a repo's spec index; ErrNoIndex when there is none.
func LoadSpecs(cache, slug string) (*Index, error) {
	ix, err := Load(SpecIndexRoot(cache, slug))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoIndex
	}
	return ix, err
}

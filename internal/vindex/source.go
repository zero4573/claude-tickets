package vindex

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileInfo is what an update needs of a document to tell whether it changed:
// a document is unchanged when MTime and Size match the indexed ones, or its
// sha256 does. A source without modification times returns MTime 0.
type FileInfo struct {
	MTime int64 // unix ns
	Size  int64
}

// Source is where an index takes its documents from. Paths are
// "/"-separated and relative to the source; the index stores them as the
// notes' Path. Implementations need not be safe for concurrent use.
type Source interface {
	// List is every indexable document.
	List() ([]string, error)
	// Normalize turns a caller-supplied path (relative, or absolute for a
	// folder source) into a source path; ok is false for a path the source
	// doesn't index.
	Normalize(path string) (rel string, ok bool)
	// Stat describes a document; an error means it is absent or unusable and
	// is dropped from the index.
	Stat(rel string) (FileInfo, error)
	// Read returns the document's bytes.
	Read(rel string) ([]byte, error)
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
	if len(parts) == 1 {
		return true
	}
	if skippedTop[parts[0]] {
		return false
	}
	// tickets/<source>/<ID>/specs-base|specs-after/...: old copies of specs.
	if len(parts) > 4 && parts[0] == "tickets" && (parts[3] == "specs-base" || parts[3] == "specs-after") {
		return false
	}
	return true
}

type vaultSource struct {
	root, realRoot string
	dirOK          map[string]bool
}

// VaultSource is the vault's notes: every regular .md file outside dot
// folders, raw/, archive/ and templates/, not under a symlinked folder.
func VaultSource(vault string) Source {
	real, _ := filepath.EvalSymlinks(vault)
	return &vaultSource{root: vault, realRoot: real, dirOK: map[string]bool{}}
}

func (v *vaultSource) List() ([]string, error) {
	var out []string
	_ = filepath.WalkDir(v.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(v.root, p)
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
	return out, nil
}

func (v *vaultSource) Normalize(f string) (string, bool) {
	f = filepath.ToSlash(f)
	if filepath.IsAbs(f) {
		if r, err := filepath.Rel(v.root, filepath.FromSlash(f)); err == nil {
			f = filepath.ToSlash(r)
		}
	}
	return f, indexable(f)
}

// Stat refuses a note under a symlinked folder: the check compares against
// the real vault path + dir, so a vault that is itself reached through a
// symlink works, while a notes folder symlinked elsewhere (even inside the
// vault) is skipped, like the full walk.
func (v *vaultSource) Stat(rel string) (FileInfo, error) {
	d := filepath.Dir(rel)
	ok, seen := v.dirOK[d]
	if !seen {
		r, err := filepath.EvalSymlinks(filepath.Join(v.root, filepath.FromSlash(d)))
		ok = err == nil && r == filepath.Join(v.realRoot, filepath.FromSlash(d))
		v.dirOK[d] = ok
	}
	if !ok {
		return FileInfo{}, fs.ErrNotExist
	}
	fi, err := os.Lstat(filepath.Join(v.root, filepath.FromSlash(rel)))
	if err != nil {
		return FileInfo{}, err
	}
	if !fi.Mode().IsRegular() {
		return FileInfo{}, fs.ErrInvalid
	}
	return FileInfo{MTime: fi.ModTime().UnixNano(), Size: fi.Size()}, nil
}

func (v *vaultSource) Read(rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(v.root, filepath.FromSlash(rel)))
}

// Package scaffold is what ct ships into a vault (assets/vault-scaffold/,
// copied by ct vault init) and how a vault's copies compare with it: each
// shipped file is missing, up to date, an unedited older version (stale),
// or edited in the vault. A copy is an unedited older version when its
// content is in the built-in history (assets/scaffold-history.json,
// generated from git; see genhistory). A copy whose content is what the
// vault's .scaffold.json records but this ct doesn't know was shipped by a
// newer ct: it's kept (Newer), never downgraded. Contents are compared
// through Key: normalized (Norm), so line endings and trailing newlines
// don't count as edits, and a .base file by its parsed YAML, so Obsidian
// reformatting it or dropping its comments doesn't either.
package scaffold

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zero4573/claude-tickets/assets"
)

// Skipped is in the scaffold but never copied: ct vault init merges it into
// .obsidian/types.json.
const Skipped = "obsidian-types.json"

// File is one shipped file of the running ct. Rel is slash-separated.
type File struct {
	Rel  string
	Data []byte
	Hash string // Key(Rel, Data)
}

// Source is what vault files are compared with.
type Source struct {
	Files   []File              // the current scaffold, sorted by Rel
	History map[string][]string // rel → normalized sha256s ever shipped
}

// Embedded is the running ct's scaffold and its history.
func Embedded() (Source, error) {
	files, err := FromFS(assets.Scaffold, "vault-scaffold")
	if err != nil {
		return Source{}, err
	}
	hist, err := ParseHistory(assets.ScaffoldHistory)
	if err != nil {
		return Source{}, fmt.Errorf("the embedded scaffold history: %w", err)
	}
	return Source{Files: files, History: hist}, nil
}

// FromFS reads the shipped files under root of fsys (all but Skipped).
func FromFS(fsys fs.FS, root string) ([]File, error) {
	var files []File
	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, root+"/")
		if rel == Skipped {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		files = append(files, File{Rel: rel, Data: data, Hash: Key(rel, data)})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	return files, err
}

// Norm is data with CRLF line endings turned into LF and its trailing
// newlines collapsed into exactly one ("" stays ""). A lone "\r" is kept.
func Norm(data []byte) []byte {
	out := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	out = bytes.TrimRight(out, "\n")
	if len(out) == 0 {
		return []byte{}
	}
	return append(out, '\n')
}

// Hash is the hex sha256 of Norm(data).
func Hash(data []byte) string {
	sum := sha256.Sum256(Norm(data))
	return hex.EncodeToString(sum[:])
}

// Key is what file contents are compared by. For a rel ending in ".base" it
// is the hex sha256 of the JSON of the parsed YAML (map keys sorted), so
// comments, indentation, quoting, style and key order don't count; values,
// list order and types do. Any other file, or a .base that doesn't parse or
// doesn't marshal, is keyed by Hash(data).
func Key(rel string, data []byte) string {
	if !strings.HasSuffix(rel, ".base") {
		return Hash(data)
	}
	var v any
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&v); err != nil {
		return Hash(data)
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return Hash(data) // several documents
	}
	j, err := json.Marshal(v)
	if err != nil {
		return Hash(data)
	}
	sum := sha256.Sum256(j)
	return hex.EncodeToString(sum[:])
}

type State int

const (
	Missing       State = iota // shipped, absent from the vault: added
	UpToDate                   // the current shipped version: nothing to do
	Stale                      // an older version this ct knows: updated
	Edited                     // matches no shipped version, or isn't a regular file: kept
	RetiredClean               // not shipped any more, unedited: kept
	RetiredEdited              // not shipped any more, edited: kept
	// Newer: what the record says was shipped, but a version this ct doesn't
	// know, so a newer ct wrote it: kept (never downgraded), its entry too
	Newer
)

// Entry is one file's state in a vault.
type Entry struct {
	Rel    string
	State  State
	Have   string // Key of the vault's copy ("" if missing or not a regular file)
	Ship   *File  // nil when not shipped any more
	Reason string // why it counts as edited, when that isn't its content
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// look reads a vault file: its hash, or why it can't be compared.
func look(dest, rel string) (exists bool, hash, reason string, err error) {
	st, err := os.Lstat(dest)
	if errors.Is(err, fs.ErrNotExist) {
		return false, "", "", nil
	}
	if err != nil {
		return false, "", "", err
	}
	if !st.Mode().IsRegular() {
		return true, "", "not a regular file", nil
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		return false, "", "", err
	}
	return true, Key(rel, data), "", nil
}

// Classify compares a vault's files with src (and what rec says was
// shipped into it). Entries are sorted by Rel; files that were shipped once
// but aren't now are listed only when they're still in the vault.
func Classify(vault string, src Source, rec Record) ([]Entry, error) {
	var entries []Entry
	shipped := map[string]bool{}
	for i := range src.Files {
		f := &src.Files[i]
		shipped[f.Rel] = true
		exists, have, reason, err := look(filepath.Join(vault, filepath.FromSlash(f.Rel)), f.Rel)
		if err != nil {
			return nil, err
		}
		e := Entry{Rel: f.Rel, Have: have, Ship: f, Reason: reason}
		r, recorded := rec.Files[f.Rel]
		switch {
		case !exists:
			e.State = Missing
		case reason != "":
			e.State = Edited
		case have == f.Hash:
			e.State = UpToDate
		case recorded && r.SHA256 == have && !contains(src.History[f.Rel], have):
			// what a newer ct shipped: never downgraded
			e.State = Newer
		case contains(src.History[f.Rel], have):
			e.State = Stale
		default:
			e.State = Edited
		}
		entries = append(entries, e)
	}

	retired := map[string]bool{}
	for rel := range src.History {
		retired[rel] = !shipped[rel]
	}
	for rel := range rec.Files {
		retired[rel] = !shipped[rel]
	}
	for rel, ok := range retired {
		// a record is the vault's own file: never look outside the vault
		if !ok || !filepath.IsLocal(filepath.FromSlash(rel)) {
			continue
		}
		exists, have, reason, err := look(filepath.Join(vault, filepath.FromSlash(rel)), rel)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		e := Entry{Rel: rel, Have: have, Reason: reason, State: RetiredEdited}
		r, recorded := rec.Files[rel]
		switch {
		case reason != "":
		case contains(src.History[rel], have):
			e.State = RetiredClean
		case recorded && r.SHA256 == have:
			e.State = Newer // shipped by a newer ct, not by this one
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Rel < entries[j].Rel })
	return entries, nil
}

// Removed are the retired files ct vault update deletes when unedited
// (the old task views, replaced by inbox.md and the per-ticket status
// file). Other retired files are only ever kept.
var Removed = []string{"done.md", "follow-ups.md", "pending.md"}

// IsRemoved says whether rel is one of Removed.
func IsRemoved(rel string) bool { return contains(Removed, rel) }

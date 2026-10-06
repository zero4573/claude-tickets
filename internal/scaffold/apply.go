package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/zero4573/claude-tickets/internal/vaultlock"
)

// Options for Apply.
type Options struct {
	DryRun  bool     // report only: nothing is created, written, renamed or locked
	Take    []string // edited files to replace with the shipped version (the old one kept as <file>.bak)
	Version string   // the ct version recorded with what it writes
	Owner   string   // the vault lock's owner, made unique per run (default ct-vault-update)
	AddOnly bool     // ct vault init: only missing files are written; stale ones are counted
}

// What Apply did (or, in a dry run, would do) to a file.
const (
	Added   = "added"
	Updated = "updated"
	Taken   = "taken"
)

// Result is a vault's files and what was done to them.
type Result struct {
	Entries  []Entry
	Done     map[string]string // rel → Added | Updated | Taken
	Warnings []string          // e.g. a record it couldn't use
}

// Count is the number of entries in a state.
func (r Result) Count(s State) int {
	n := 0
	for _, e := range r.Entries {
		if e.State == s {
			n++
		}
	}
	return n
}

// Apply brings a vault's shipped files up to date: missing files are added,
// stale ones replaced (unless AddOnly), taken ones replaced after keeping
// the old copy as <file>.bak; edited and retired ones are left alone. It
// writes under the vault lock, then records what the vault now holds in
// .scaffold.json (only when that changed).
func Apply(vault string, src Source, o Options) (Result, error) {
	ship := map[string]bool{}
	for _, f := range src.Files {
		ship[f.Rel] = true
	}
	take := map[string]bool{}
	for _, t := range o.Take {
		rel, err := takeRel(vault, t, func(rel string) bool { return ship[rel] || len(src.History[rel]) > 0 })
		if err != nil {
			return Result{}, err
		}
		switch {
		case ship[rel]:
			take[rel] = true
		case len(src.History[rel]) > 0:
			return Result{}, fmt.Errorf("%s isn't shipped any more", rel)
		default:
			return Result{}, fmt.Errorf("not a file ct ships: %s", rel)
		}
	}

	if !o.DryRun {
		// One owner per run: Acquire lets its holder straight back in, so a
		// shared name would let two runs write at once
		owner := o.Owner
		if owner == "" {
			owner = "ct-vault-update"
		}
		owner = fmt.Sprintf("%s-%d-%s", owner, os.Getpid(), randomID())
		if err := vaultlock.Acquire(vault, owner); err != nil {
			return Result{}, err
		}
		defer func() { _ = vaultlock.Release(vault, owner) }()
	}

	rec, state, warning := loadRecord(vault)
	res := Result{Done: map[string]string{}}
	if warning != "" {
		res.Warnings = append(res.Warnings, warning)
	}
	entries, err := Classify(vault, src, rec)
	if err != nil {
		return Result{}, err
	}
	res.Entries = entries

	// Takes are checked before anything is written
	for _, e := range entries {
		if !take[e.Rel] || e.State != Edited && e.State != Newer {
			continue
		}
		if e.Reason != "" {
			return Result{}, fmt.Errorf("%s: %s; move it away first", e.Rel, e.Reason)
		}
		bak := filepath.Join(vault, filepath.FromSlash(e.Rel)) + ".bak"
		if _, err := os.Lstat(bak); err == nil {
			return Result{}, fmt.Errorf("%s.bak exists; move it away first", e.Rel)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return Result{}, err
		}
	}

	for _, e := range entries {
		switch {
		case e.State == Missing:
			res.Done[e.Rel] = Added
		case e.State == Stale && !o.AddOnly:
			res.Done[e.Rel] = Updated
		case (e.State == Edited || e.State == Newer) && take[e.Rel]:
			res.Done[e.Rel] = Taken
		default:
			continue
		}
		if o.DryRun {
			continue
		}
		dest := filepath.Join(vault, filepath.FromSlash(e.Rel))
		// a replaced file keeps its mode
		var mode fs.FileMode
		keep := false
		if st, err := os.Lstat(dest); err == nil {
			mode, keep = st.Mode(), true
		}
		if res.Done[e.Rel] == Taken {
			if err := os.Rename(dest, dest+".bak"); err != nil {
				return res, err
			}
		}
		if err := writeAtomic(dest, e.Ship.Data, mode, keep); err != nil {
			return res, err
		}
	}
	if o.DryRun {
		return res, nil
	}

	// The record: what each unedited file now is. Edited and retired files
	// keep their entry (the last version shipped), paths that are gone lose it.
	files := map[string]Shipped{}
	set := func(rel, hash string) {
		if old, ok := rec.Files[rel]; ok && old.SHA256 == hash {
			files[rel] = old
		} else {
			files[rel] = Shipped{SHA256: hash, Version: o.Version}
		}
	}
	for _, e := range entries {
		switch {
		case res.Done[e.Rel] != "":
			set(e.Rel, e.Ship.Hash)
		case e.State == UpToDate, e.State == Stale:
			set(e.Rel, e.Have)
		default:
			if old, ok := rec.Files[e.Rel]; ok {
				files[e.Rel] = old
			}
		}
	}
	write := false
	switch state {
	case recordAbsent:
		write = len(files) > 0
	case recordOK:
		write = !sameRecord(rec.Files, files)
	case recordInvalid:
		write = len(res.Done) > 0
	case recordNewer:
	}
	if write {
		if err := saveRecord(vault, files); err != nil {
			return res, err
		}
	}
	return res, nil
}

func sameRecord(a, b map[string]Shipped) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// takeRel is the vault-relative, slash-separated path a --take names: a
// path relative to the vault root, or one (absolute, or relative to the
// current directory) that's inside the vault. known says whether a
// vault-relative path is one ct ships or has shipped. Paths outside the
// vault are refused.
func takeRel(vault, t string, known func(rel string) bool) (string, error) {
	clean := filepath.Clean(t)
	local := !filepath.IsAbs(clean) && filepath.IsLocal(clean)
	if local && known(filepath.ToSlash(clean)) {
		return filepath.ToSlash(clean), nil
	}
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", err
	}
	if rel, ok := inVault(vault, abs); ok {
		return rel, nil
	}
	if !local {
		return "", fmt.Errorf("%s is outside the vault", t)
	}
	return filepath.ToSlash(clean), nil
}

// inVault is abs relative to the vault, when it's inside it: lexically, or
// else with symlinks resolved (e.g. a vault reached through a link).
func inVault(vault, abs string) (string, bool) {
	root, err := filepath.Abs(vault)
	if err != nil {
		return "", false
	}
	rel := func(root, p string) (string, bool) {
		r, err := filepath.Rel(root, p)
		if err != nil || !filepath.IsLocal(r) {
			return "", false
		}
		return filepath.ToSlash(r), true
	}
	if r, ok := rel(root, abs); ok {
		return r, true
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	// the file itself may be a link or missing: resolve its folder
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", false
	}
	return rel(realRoot, filepath.Join(dir, filepath.Base(abs)))
}

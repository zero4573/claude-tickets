package links

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zero4573/claude-tickets/internal/fsx"
)

// move is one file of a MoveTree: absolute paths, and vault-relative
// "/"-separated ones.
type move struct {
	src, dst       string
	srcRel, dstRel string
}

// MoveTree moves files (absolute paths under srcDir; nil: every file in
// it) to the same relative paths under dstDir, keeping links working:
// path-qualified links to them are rewritten first, then the files are
// renamed, last (relative to srcDir, e.g. the folder's note) after all the
// others. Bare-name links stay as they are: the names don't change. A
// re-run completes an interrupted move: a file whose new path a link
// already names moves too. It fails, before changing anything, if a
// destination exists. Empty folders left in srcDir are removed. It returns
// the files moved, links rewritten and notes changed (MoveResult).
func MoveTree(vault, srcDir, dstDir string, files []string, last string) (res MoveResult, err error) {
	vault, srcDir, dstDir = resolvePath(vault), resolvePath(srcDir), resolvePath(dstDir)
	if !fsx.Inside(srcDir, vault) || !fsx.Inside(dstDir, vault) || fsx.Within(dstDir, srcDir) || fsx.Within(srcDir, dstDir) {
		return MoveResult{}, errors.New("move: both folders must be inside the vault, and apart")
	}
	var all []string
	_ = filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			all = append(all, p)
		}
		return nil
	})
	rel := func(p string) string {
		r, _ := filepath.Rel(vault, p)
		return filepath.ToSlash(r)
	}
	plan := func(src string) move {
		r, _ := filepath.Rel(srcDir, src)
		dst := filepath.Join(dstDir, r)
		return move{src, dst, rel(src), rel(dst)}
	}
	want := map[string]bool{}
	if files == nil {
		for _, f := range all {
			want[f] = true
		}
	} else {
		for _, f := range files {
			want[resolvePath(f)] = true
		}
		for _, f := range pendingMoves(vault, all, plan) {
			want[f] = true
		}
	}
	var moves []move
	var final *move
	for _, f := range all {
		if !want[f] {
			continue
		}
		m := plan(f)
		if _, err := os.Lstat(m.dst); err == nil {
			return MoveResult{}, fmt.Errorf("move: destination exists: %s", m.dstRel)
		}
		if last != "" && f == filepath.Join(srcDir, last) {
			final = &m
			continue
		}
		moves = append(moves, m)
	}
	if final != nil {
		moves = append(moves, *final)
	}
	if len(moves) == 0 {
		fsx.RemoveEmptyDirs(srcDir)
		return MoveResult{}, nil
	}
	defer fsx.RemoveEmptyDirs(srcDir)
	return applyMoves(vault, moves)
}

// Tree is one folder of a MoveTrees: every file of Src goes to the same
// relative path under Dst; Last (relative to Src, e.g. the folder's note)
// goes after the other folders' files.
type Tree struct{ Src, Dst, Last string }

// MoveTrees is MoveTree (every file) for several folders at once, with one
// pass over the vault's notes for their links. Each Last file moves after
// every other file. It fails, before changing anything, if a destination
// exists, or a folder isn't inside the vault and apart from its
// destination.
func MoveTrees(vault string, trees []Tree) (MoveResult, error) {
	vault = resolvePath(vault)
	rel := func(p string) string {
		r, _ := filepath.Rel(vault, p)
		return filepath.ToSlash(r)
	}
	var moves, finals []move
	var srcs []string
	for _, t := range trees {
		srcDir, dstDir := resolvePath(t.Src), resolvePath(t.Dst)
		if !fsx.Inside(srcDir, vault) || !fsx.Inside(dstDir, vault) || fsx.Within(dstDir, srcDir) || fsx.Within(srcDir, dstDir) {
			return MoveResult{}, errors.New("move: both folders must be inside the vault, and apart")
		}
		srcs = append(srcs, srcDir)
		werr := filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			r, _ := filepath.Rel(srcDir, p)
			dst := filepath.Join(dstDir, r)
			m := move{p, dst, rel(p), rel(dst)}
			if _, err := os.Lstat(dst); err == nil {
				return fmt.Errorf("move: destination exists: %s", m.dstRel)
			}
			if t.Last != "" && p == filepath.Join(srcDir, t.Last) {
				finals = append(finals, m)
			} else {
				moves = append(moves, m)
			}
			return nil
		})
		if werr != nil {
			return MoveResult{}, werr
		}
	}
	moves = append(moves, finals...)
	defer func() {
		for _, s := range srcs {
			fsx.RemoveEmptyDirs(s)
		}
	}()
	if len(moves) == 0 {
		return MoveResult{}, nil
	}
	return applyMoves(vault, moves)
}

// applyMoves rewrites the path-qualified links to the files moves names,
// then moves them, in order.
func applyMoves(vault string, moves []move) (res MoveResult, err error) {
	rel := func(p string) string {
		r, _ := filepath.Rel(vault, p)
		return filepath.ToSlash(r)
	}
	changed := map[string]bool{}
	defer func() { res.Changed = sortedKeys(changed) }()
	forms := make([][]string, len(moves))
	for i, m := range moves {
		forms[i] = pathForms(m.srcRel)
	}
	for _, f := range Files(vault) {
		if !strings.HasSuffix(f, ".md") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		text := string(data)
		out := rewriteLinks(f, text, func(m []string, _ at) string {
			t := strings.ToLower(strings.TrimLeft(target(m), "/"))
			if !strings.Contains(t, "/") {
				return m[0]
			}
			for i, mv := range moves {
				if namesPath(t, forms[i]) {
					res.Rewritten++
					return m[1] + "[[" + linkPath(mv.dstRel) + m[3] + m[4] + "]]"
				}
			}
			return m[0]
		})
		if out != text {
			if err := writeNote(f, []byte(out)); err != nil {
				return res, err
			}
			changed[rel(f)] = true
		}
	}
	for _, m := range moves {
		if err := os.MkdirAll(filepath.Dir(m.dst), 0o755); err != nil {
			return res, err
		}
		if err := moveFile(m.src, m.dst); err != nil {
			return res, err
		}
		res.Moves = append(res.Moves, Moved{m.srcRel, m.dstRel})
	}
	return res, nil
}

// pendingMoves is the files (of all) whose new path an unresolved
// path-qualified link already names: their links were rewritten by a
// MoveTree that stopped before it moved them.
func pendingMoves(vault string, all []string, plan func(string) move) []string {
	ix := NewIndex(vault)
	var dangling []string
	for _, f := range notes(vault) {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, l := range linksIn(f, string(data)) {
			t := strings.ToLower(strings.TrimLeft(target(l.parts), "/"))
			if strings.Contains(t, "/") && len(ix.Resolve(t)) == 0 {
				dangling = append(dangling, t)
			}
		}
	}
	var out []string
	for _, f := range all {
		forms := pathForms(plan(f).dstRel)
		for _, t := range dangling {
			if namesPath(t, forms) {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

package ticketmigrate

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zero4573/claude-tickets/internal/links"
	tnote "github.com/zero4573/claude-tickets/internal/note"
)

// Move is one ticket folder MigrateLayout moves (or, dry, would move):
// vault-relative, "/"-separated folders.
type Move struct {
	ID, Source string
	From, To   string
}

// LayoutResult is what MigrateLayout did.
type LayoutResult struct {
	Moves     []Move
	Files     []links.Moved // the files moved (empty on a dry run)
	Rewritten int           // path-qualified links rewritten
	Changed   []string      // notes whose links were rewritten
	Warnings  []string
}

// Rels are the vault-relative paths the move wrote or removed: for the
// search index.
func (r LayoutResult) Rels() []string {
	var out []string
	for _, f := range r.Files {
		out = append(out, f.From, f.To)
	}
	return append(out, r.Changed...)
}

// moveTrees is the move of MigrateLayout: a test seam.
var moveTrees = links.MoveTrees

// MigrateLayout moves an old vault's ticket folders, tickets/<ID>/ and
// archive/tickets/<ID>/, under their source: tickets/<source>/<ID>/ and
// archive/tickets/<source>/<ID>/. The source is the note's frontmatter
// source when it names one (ct sync and ct new write it), else the one
// tickets/.sources.json gives the ID (tnote.GuessSource); a folder whose
// source can't be told, or whose new place is taken, stays where it is
// with a warning. Path-qualified links to the files are rewritten (bare
// [[name]] links don't change); the note moves last, so a run that stops
// halfway is completed by the next. dryRun only lists the moves. The
// caller holds the vault lock.
func MigrateLayout(vault string, dryRun bool) (LayoutResult, error) {
	var res LayoutResult
	var trees []links.Tree
	plan := func(root, rootRel string, l tnote.Loc) {
		src := strings.TrimSpace(tnote.Get(l.Note(), "source"))
		from := rootRel + "/" + l.ID
		if !tnote.ValidSource(src) {
			if src != "" {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s/%s.md: source %q can't name a folder; placed by its ID", from, l.ID, src))
			}
			src = tnote.GuessSource(vault, l.ID)
		}
		to := filepath.Join(root, src, l.ID)
		res.Moves = append(res.Moves, Move{ID: l.ID, Source: src, From: from, To: rootRel + "/" + src + "/" + l.ID})
		trees = append(trees, links.Tree{Src: l.Dir, Dst: to, Last: l.ID + ".md"})
	}
	for _, r := range []struct{ root, rel string }{
		{tnote.TicketsDir(vault), "tickets"},
		{tnote.ArchiveRoot(vault), "archive/tickets"},
	} {
		for _, l := range dirsIn(r.root) {
			plan(r.root, r.rel, l)
		}
	}
	// One folder at a time would read every note once per folder; all at
	// once fails as a whole on one taken destination, so take those out first
	var ok []links.Tree
	var moves []Move
	for i, t := range trees {
		if taken := takenIn(t); taken != "" {
			r, _ := filepath.Rel(vault, taken)
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: not moved: %s exists already; merge the two folders by hand", res.Moves[i].From, filepath.ToSlash(r)))
			continue
		}
		ok = append(ok, t)
		moves = append(moves, res.Moves[i])
	}
	res.Moves = moves
	if dryRun || len(ok) == 0 {
		return res, nil
	}
	mv, err := moveTrees(vault, ok)
	res.Files, res.Rewritten, res.Changed = mv.Moves, mv.Rewritten, mv.Changed
	return res, err
}

// dirsIn is the old-layout ticket folders right under root.
func dirsIn(root string) []tnote.Loc {
	var out []tnote.Loc
	for _, l := range tnote.DirsUnder(root) {
		if l.Source == "" {
			out = append(out, l)
		}
	}
	return out
}

// takenIn is the vault file a tree's move would overwrite ("" when none).
func takenIn(t links.Tree) string {
	var taken string
	_ = filepath.WalkDir(t.Src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || taken != "" {
			return nil
		}
		r, _ := filepath.Rel(t.Src, p)
		if dst := filepath.Join(t.Dst, r); isFile(dst) {
			taken = dst
		}
		return nil
	})
	return taken
}

func isFile(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

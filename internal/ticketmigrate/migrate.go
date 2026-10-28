package ticketmigrate

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zero4573/claude-tickets/internal/fsx"
	tnote "github.com/zero4573/claude-tickets/internal/note"
)

// FileChange is a converted file: its vault-relative path (slash form), and
// the bytes before and after.
type FileChange struct {
	Rel      string
	Old, New []byte
}

// TicketChange is what migrating one ticket did (or, dry, would do).
type TicketChange struct {
	ID          string
	Dir         string // the ticket's folder, vault-relative ("/"-separated)
	StatusAdded bool   // <ID>-status.md written
	StatusRel   string
	Files       []FileChange // converted files, board.md first
}

// Rels are the vault-relative paths written.
func (t TicketChange) Rels() []string {
	var r []string
	if t.StatusAdded {
		r = append(r, t.StatusRel)
	}
	for _, f := range t.Files {
		r = append(r, f.Rel)
	}
	return r
}

// Result of Migrate.
type Result struct {
	Tickets  []TicketChange // only tickets with a change
	Warnings []string
	Migrated int // tickets with a change
	Current  int // non-closed tickets with nothing to do
	Closed   int // skipped
}

// writeStatus writes a new status file: a test seam.
var writeStatus = fsx.WriteFileAtomic

// beforeWrite is a test seam: called just before a converted file is
// re-read and written.
var beforeWrite func(abs string)

// Migrate brings every non-closed ticket of a vault to the checklist
// formats: <ID>-status.md added from statusTpl (TICKET-ID and {{date}}
// replaced), board.md, reviews/*.md and backlog.md converted. dryRun writes
// nothing. The caller holds the vault lock.
func Migrate(vault string, statusTpl []byte, today string, dryRun bool) (Result, error) {
	var res Result
	for _, l := range tnote.TicketDirs(vault) { // sorted by ID
		id := l.ID
		relDir := filepath.ToSlash(mustRel(vault, l.Dir))
		note, err := os.ReadFile(l.Note())
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s/%s.md: %v; skipped", relDir, id, err))
			continue
		}
		if status(note) == "closed" {
			res.Closed++
			continue
		}
		tc, err := migrateTicket(vault, l.Dir, relDir, id, statusTpl, today, dryRun, &res)
		if err != nil {
			return res, err
		}
		if tc.StatusAdded || len(tc.Files) > 0 {
			res.Migrated++
			res.Tickets = append(res.Tickets, tc)
		} else {
			res.Current++
		}
	}
	return res, nil
}

func mustRel(base, p string) string {
	r, err := filepath.Rel(base, p)
	if err != nil {
		return p
	}
	return r
}

// status is the note's frontmatter status ("" with no frontmatter).
func status(note []byte) string {
	s := strings.TrimPrefix(string(note), "\xef\xbb\xbf")
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t\r") != "---" {
		return ""
	}
	for _, l := range lines[1:] {
		l = strings.TrimRight(l, " \t\r")
		if l == "---" {
			break
		}
		if v, ok := strings.CutPrefix(l, "status:"); ok {
			return strings.ToLower(strings.Trim(strings.TrimSpace(v), `"'`))
		}
	}
	return ""
}

func migrateTicket(vault, dir, relDir, id string, statusTpl []byte, today string, dryRun bool, res *Result) (TicketChange, error) {
	tc := TicketChange{ID: id, Dir: relDir, StatusRel: path.Join(relDir, id+"-status.md")}
	warn := func(format string, a ...any) { res.Warnings = append(res.Warnings, fmt.Sprintf(format, a...)) }

	statusFailed := false
	sp := filepath.Join(dir, id+"-status.md")
	if _, err := os.Lstat(sp); errors.Is(err, fs.ErrNotExist) {
		data := strings.NewReplacer("TICKET-ID", id, "{{date}}", today).Replace(string(statusTpl))
		if !dryRun {
			if err := writeStatus(sp, []byte(data), 0o644); err != nil {
				warn("%s: %v; not written (the ticket's other files are converted; run it again to add it)", tc.StatusRel, err)
				statusFailed = true
			}
		}
		tc.StatusAdded = !statusFailed
	} else if err != nil {
		return tc, err
	}

	convert := func(rel string, conv func([]byte) Converted) {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		vrel := path.Join(relDir, rel)
		data, err := os.ReadFile(abs)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				warn("%s: %v; skipped", vrel, err)
			}
			return
		}
		c := conv(data)
		for _, p := range c.Problems {
			warn("%s:%d: not converted (%s); convert it by hand", vrel, p.Line, p.Why)
		}
		for _, p := range c.Warnings {
			warn("%s:%d: %s", vrel, p.Line, p.Why)
		}
		if !c.Changed {
			return
		}
		if !dryRun {
			if beforeWrite != nil {
				beforeWrite(abs)
			}
			// just before the rename: the file must still be what was read
			if now, err := os.ReadFile(abs); err != nil || !bytes.Equal(now, data) {
				warn("%s: changed during the update; run it again", vrel)
				return
			}
			if err := fsx.WriteNote(abs, c.Data); err != nil {
				warn("%s: %v; not converted", vrel, err)
				return
			}
		}
		tc.Files = append(tc.Files, FileChange{Rel: vrel, Old: data, New: c.Data})
	}
	convert("board.md", ConvertBoard)
	convert("backlog.md", ConvertBacklog)
	revs, _ := filepath.Glob(filepath.Join(dir, "reviews", "*.md"))
	sort.Strings(revs)
	for _, r := range revs {
		if st, err := os.Stat(r); err == nil && st.Mode().IsRegular() {
			convert("reviews/"+filepath.Base(r), ConvertReview)
		}
	}
	return tc, nil
}

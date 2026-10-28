package note

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The vault keeps a ticket's folder under its source:
// tickets/<source>/<ID>/ (its note tickets/<source>/<ID>/<ID>.md), and an
// archived one under archive/tickets/<source>/<ID>/. <source> is the
// source's key in tickets/.sources.json, which is also the note's
// frontmatter source (jira, manual, ...). Vaults from before ct put the
// folders straight under tickets/ (tickets/<ID>/): they are still read
// until ct vault update moves them (MigrateLayout).

var sourceRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// ValidSource reports whether s can name a source folder: a source key
// (letters, digits, - and _, not starting with either) that isn't a
// ticket ID, so tickets/<source>/ and an old tickets/<ID>/ never mix up.
func ValidSource(s string) bool { return sourceRe.MatchString(s) && !ValidKey(s) }

// TicketsDir is the vault's tickets/ folder.
func TicketsDir(vault string) string { return filepath.Join(vault, "tickets") }

// SourceDir is a source's folder of tickets.
func SourceDir(vault, source string) string { return filepath.Join(TicketsDir(vault), source) }

// SourcePath is where the note of a ticket of source goes:
// tickets/<source>/<ID>/<ID>.md.
func SourcePath(vault, source, id string) string {
	return filepath.Join(SourceDir(vault, source), id, id+".md")
}

// Loc is a ticket folder found in the vault.
type Loc struct {
	ID     string
	Source string // the source folder; "" for an old tickets/<ID>/ folder
	Dir    string // absolute
}

// Note is the folder's ticket note (which may not exist).
func (l Loc) Note() string { return filepath.Join(l.Dir, l.ID+".md") }

// sourceDirs is the source folders under root (tickets/ or archive/tickets/), sorted.
func sourceDirs(root string) []string {
	entries, _ := os.ReadDir(root)
	var out []string
	for _, e := range entries {
		if e.IsDir() && ValidSource(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

// DirsUnder is the ticket folders under root, both layouts, sorted by ID
// (then source; an old folder before the source folders).
func DirsUnder(root string) []Loc {
	var out []Loc
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.IsDir() && ValidKey(e.Name()) {
			out = append(out, Loc{ID: e.Name(), Dir: filepath.Join(root, e.Name())})
		}
	}
	for _, src := range sourceDirs(root) {
		sub, _ := os.ReadDir(filepath.Join(root, src))
		for _, e := range sub {
			if e.IsDir() && ValidKey(e.Name()) {
				out = append(out, Loc{ID: e.Name(), Source: src, Dir: filepath.Join(root, src, e.Name())})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Source < out[j].Source
	})
	return out
}

// TicketDirs is every ticket folder of the vault (with or without its
// note), both layouts, sorted by ID.
func TicketDirs(vault string) []Loc { return DirsUnder(TicketsDir(vault)) }

// TicketNotes is the ticket notes of the vault, sorted by ID: one per ID,
// the one Find picks.
func TicketNotes(vault string) []Loc {
	var out []Loc
	seen := map[string]bool{}
	for _, l := range TicketDirs(vault) {
		if seen[l.ID] {
			continue
		}
		if p := Find(vault, l.ID); p != "" {
			seen[l.ID] = true
			out = append(out, Loc{ID: l.ID, Source: sourceOfDir(vault, filepath.Dir(p)), Dir: filepath.Dir(p)})
		}
	}
	return out
}

func sourceOfDir(vault, dir string) string {
	parent := filepath.Dir(dir)
	if parent == TicketsDir(vault) {
		return ""
	}
	return filepath.Base(parent)
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// Find is the note of ticket id, wherever it is: under a source folder
// (the first, by name, when several have one), else an old
// tickets/<ID>/<ID>.md; "" when the vault has none.
func Find(vault, id string) string {
	if !ValidKey(id) {
		return ""
	}
	for _, src := range sourceDirs(TicketsDir(vault)) {
		if p := SourcePath(vault, src, id); isFile(p) {
			return p
		}
	}
	if p := filepath.Join(TicketsDir(vault), id, id+".md"); isFile(p) {
		return p
	}
	return ""
}

// TicketPath is ticket id's note: the one Find finds, else where a new
// note of it goes (under GuessSource's folder).
func TicketPath(vault, id string) string {
	if p := Find(vault, id); p != "" {
		return p
	}
	return SourcePath(vault, GuessSource(vault, id), id)
}

// TicketDir is a ticket's folder: its note, the lead's plan, board, task,
// contract and review files, kb-drafts/ and logs/.
func TicketDir(vault, id string) string { return filepath.Dir(TicketPath(vault, id)) }

// RelDir is a ticket's folder relative to the vault, "/"-separated
// (tickets/jira/PROJ-12).
func RelDir(vault, id string) string {
	r, err := filepath.Rel(vault, TicketDir(vault, id))
	if err != nil {
		return path.Join("tickets", GuessSource(vault, id), id)
	}
	return filepath.ToSlash(r)
}

// SourceOf is the source folder ticket id belongs in: the one its folder
// is in, else its note's frontmatter source, else GuessSource.
func SourceOf(vault, id string) string {
	if p := Find(vault, id); p != "" {
		if s := sourceOfDir(vault, filepath.Dir(p)); s != "" {
			return s
		}
		if s := strings.TrimSpace(Get(p, "source")); ValidSource(s) {
			return s
		}
	}
	return GuessSource(vault, id)
}

// ArchiveRoot is the vault's archive/tickets/.
func ArchiveRoot(vault string) string { return filepath.Join(vault, "archive", "tickets") }

// ArchiveDir is where ct clean puts a ticket's folder it archives (manual
// tickets), or the files other notes still link from a ticket it removes:
// the ticket's folder in the archive when there is one, else
// archive/tickets/<source>/<ID>.
func ArchiveDir(vault, id string) string {
	for _, l := range DirsUnder(ArchiveRoot(vault)) {
		if l.ID == id {
			return l.Dir
		}
	}
	return filepath.Join(ArchiveRoot(vault), SourceOf(vault, id), id)
}

// ArchivedIDs is the IDs of the tickets with a folder in the archive.
func ArchivedIDs(vault string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range DirsUnder(ArchiveRoot(vault)) {
		if !seen[l.ID] {
			seen[l.ID] = true
			out = append(out, l.ID)
		}
	}
	return out
}

// Sources is the vault's tickets/.sources.json, by source key (nil when
// it is missing or doesn't parse).
func Sources(vault string) map[string]SourceConf {
	data, err := os.ReadFile(filepath.Join(TicketsDir(vault), ".sources.json"))
	if err != nil {
		return nil
	}
	var cfg struct {
		Sources map[string]SourceConf `json:"sources"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	return cfg.Sources
}

// SourceConf is what the layout reads of a source in .sources.json.
type SourceConf struct {
	Enabled  *bool  `json:"enabled"`
	IDPrefix string `json:"idPrefix"`
}

// GuessSource is the source a ticket with no note yet belongs to, by its
// ID: the source whose idPrefix starts it (the manual source's is MAN when
// unset), else the first source that uses the source's own keys (no
// idPrefix), an enabled one first, else manual.
func GuessSource(vault, id string) string {
	srcs := Sources(vault)
	keys := make([]string, 0, len(srcs))
	for k := range srcs {
		if ValidSource(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	prefix := func(k string) string {
		if p := srcs[k].IDPrefix; p != "" || k != "manual" {
			return p
		}
		return "MAN"
	}
	for _, k := range keys {
		if p := prefix(k); p != "" && strings.HasPrefix(id, p+"-") {
			return k
		}
	}
	if _, ok := srcs["manual"]; !ok && strings.HasPrefix(id, "MAN-") {
		return "manual"
	}
	for _, enabledOnly := range []bool{true, false} {
		for _, k := range keys {
			if c := srcs[k]; prefix(k) == "" && (!enabledOnly || c.Enabled == nil || *c.Enabled) {
				return k
			}
		}
	}
	return "manual"
}

// RelTicket parses a vault-relative "/"-separated path inside a ticket
// folder: tickets/<source>/<ID>/<rest> (or an old tickets/<ID>/<rest>).
// It gives the ID, the folder ("tickets/jira/PROJ-12") and the rest ("" for
// the folder itself).
func RelTicket(rel string) (id, dir, rest string, ok bool) {
	p := strings.Split(rel, "/")
	if len(p) < 2 || p[0] != "tickets" {
		return "", "", "", false
	}
	if ValidKey(p[1]) {
		return p[1], strings.Join(p[:2], "/"), strings.Join(p[2:], "/"), true
	}
	if len(p) >= 3 && ValidSource(p[1]) && ValidKey(p[2]) {
		return p[2], strings.Join(p[:3], "/"), strings.Join(p[3:], "/"), true
	}
	return "", "", "", false
}

// RelNote is the ID when rel (vault-relative, "/"-separated) is a ticket
// note: tickets/<source>/<ID>/<ID>.md (or an old tickets/<ID>/<ID>.md).
func RelNote(rel string) (string, bool) {
	id, _, rest, ok := RelTicket(rel)
	if ok && rest == id+".md" {
		return id, true
	}
	return "", false
}

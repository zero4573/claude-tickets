// Package note reads the vault's notes: frontmatter fields and the ticket
// notes (tickets/<ID>/<ID>.md).
package note

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var keyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*(-[A-Z0-9]+)+$`)

// ValidKey reports whether s is a local ticket ID: the source's own key
// (Jira PROJ-12), or <idPrefix>-<native id> (SNOW-INC0012345, MAN-7).
func ValidKey(s string) bool { return keyRe.MatchString(s) }

func TicketPath(vault, id string) string {
	return filepath.Join(vault, "tickets", id, id+".md")
}

// TicketDir is a ticket's folder: its note, hand-off files, kb-drafts/ and
// logs/.
func TicketDir(vault, id string) string { return filepath.Dir(TicketPath(vault, id)) }

// ArchiveDir is where ct clean puts a ticket's folder it archives (manual
// tickets), or the files other notes still link from a ticket it removes.
func ArchiveDir(vault, id string) string { return filepath.Join(vault, "archive", "tickets", id) }

// ArchivedIDs is the IDs of the tickets with a folder in the archive.
func ArchivedIDs(vault string) []string {
	dirs, _ := os.ReadDir(filepath.Dir(ArchiveDir(vault, "X")))
	var out []string
	for _, d := range dirs {
		if d.IsDir() && ValidKey(d.Name()) {
			out = append(out, d.Name())
		}
	}
	return out
}

// IsTicketNote reports whether file is a ticket's note (<ID>/<ID>.md).
func IsTicketNote(file string) bool {
	id := strings.TrimSuffix(filepath.Base(file), ".md")
	return ValidKey(id) && filepath.Base(file) == id+".md" && filepath.Base(filepath.Dir(file)) == id
}

// Synced reports whether a ticket note's frontmatter is a synced ticket's
// (ct sync or the ticket-sync skill writes it), not a manual one's.
func Synced(fm map[string]string) bool {
	s := fm["source"]
	return s != "" && s != "manual"
}

// SyncOwnedFields are the frontmatter fields ct sync rewrites on a synced
// ticket's refresh (besides source-*): links in them come back as sync
// writes them, whatever else edits them.
var SyncOwnedFields = []string{"parent", "children", "blocked-by", "blocks", "related", "blocked", "covers", "covered-by"}

// SyncOwned reports whether a place in a ticket note with frontmatter fm
// is sync's own: on a synced ticket, a sync-owned frontmatter field
// (field, "" outside the frontmatter) or the source block.
func SyncOwned(fm map[string]string, field string, sourceBlock bool) bool {
	if !Synced(fm) {
		return false
	}
	if sourceBlock || strings.HasPrefix(field, "source-") {
		return true
	}
	for _, f := range SyncOwnedFields {
		if f == field {
			return true
		}
	}
	return false
}

// Frontmatter returns a note's top-level scalar fields (the text after
// "key:", surrounding quotes stripped), as the YAML block at the top of
// the note has them. A missing note or one without frontmatter gives an
// empty map. Lists and nested values come back as their raw first line.
func Frontmatter(file string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(file)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	first := true
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		if first {
			line = strings.TrimPrefix(line, "\ufeff")
			if line != "---" {
				return out
			}
			first = false
			continue
		}
		if line == "---" {
			break
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if _, seen := out[key]; seen {
			continue
		}
		out[key] = unquote(strings.TrimLeft(val, " \t"))
	}
	return out
}

func Get(file, field string) string { return Frontmatter(file)[field] }

func unquote(v string) string {
	v = strings.TrimRight(v, " \t")
	// A YAML quoted scalar: decode its escapes (\" \\ '' ...)
	if n := len(v); n >= 2 && (v[0] == '"' && v[n-1] == '"' || v[0] == '\'' && v[n-1] == '\'') {
		var s string
		if yaml.Unmarshal([]byte(v), &s) == nil {
			return s
		}
	}
	if len(v) > 0 && (v[0] == '"' || v[0] == '\'') {
		v = v[1:]
	}
	if n := len(v); n > 0 && (v[n-1] == '"' || v[n-1] == '\'') {
		v = v[:n-1]
	}
	return v
}

// Ignored reports whether a ticket note sets ignore: true and any
// ignore-until date (yyyy-MM-dd) hasn't passed yet.
func Ignored(file string) bool {
	fm := Frontmatter(file)
	if fm["ignore"] != "true" {
		return false
	}
	until := fm["ignore-until"]
	return until == "" || until >= time.Now().Format("2006-01-02")
}

type Ticket struct {
	ID, Status, Summary string
}

// List returns the vault's tickets sorted by ID; done and closed ones only
// with all. Status gets ", ignored" when the note sets ignore: true.
func List(vault string, all bool) []Ticket {
	dirs, _ := os.ReadDir(filepath.Join(vault, "tickets"))
	var out []Ticket
	for _, d := range dirs {
		id := d.Name()
		if !d.IsDir() || !ValidKey(id) {
			continue
		}
		file := TicketPath(vault, id)
		if _, err := os.Stat(file); err != nil {
			continue
		}
		fm := Frontmatter(file)
		status := fm["status"]
		if !all && (status == "done" || status == "closed") {
			continue
		}
		if fm["ignore"] == "true" {
			status += ", ignored"
		}
		if status == "" {
			status = "-"
		}
		out = append(out, Ticket{id, status, strings.ReplaceAll(fm["summary"], "\t", " ")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

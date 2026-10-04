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

// TicketPath is a ticket's note: <vault>/tickets/<ID>/<ID>.md.
func TicketPath(vault, id string) string {
	return filepath.Join(vault, "tickets", id, id+".md")
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
		line := sc.Text()
		if first {
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

// Get is one frontmatter field ("" if absent).
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

// Ticket is one line of the vault's ticket list.
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

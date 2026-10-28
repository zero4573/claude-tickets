package notesec

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	tnote "github.com/zero4573/claude-tickets/internal/note"
)

// Ctx is where a write comes from.
type Ctx struct {
	Vault     string
	Workspace string // the ticket workspace's ID, "" outside one
	Save      bool   // /tickets:save's shared-notes mode
}

// RefusedError is a write the rules don't allow (ct exits 3).
type RefusedError struct{ Msg string }

func (e *RefusedError) Error() string { return e.Msg }

func refuse(format string, a ...any) error { return &RefusedError{fmt.Sprintf(format, a...)} }

// WorkspaceID is the ID of the nearest workspace.json with an id at or
// above dir, "" when there is none.
func WorkspaceID(dir string) string {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if data, err := os.ReadFile(filepath.Join(dir, "workspace.json")); err == nil {
			var w struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(data, &w) == nil && w.ID != "" {
				return w.ID
			}
		}
		up := filepath.Dir(dir)
		if up == dir {
			return ""
		}
		dir = up
	}
}

var (
	identityFields = map[string]bool{"title": true, "type": true, "ticket-type": true, "created": true,
		"parent": true, "children": true, "blocked-by": true, "blocks": true, "related": true,
		"covers": true, "covered-by": true}
	contentSections = map[string]bool{"description": true, "acceptance criteria": true, "context / links": true}
	workSections    = map[string]bool{"workspace": true, "tasks": true, "questions": true, "follow-ups": true, "review": true}
)

func protectedField(k string) bool {
	k = strings.ToLower(k)
	return identityFields[k] || strings.HasPrefix(k, "source") || strings.HasPrefix(k, "ignore")
}

func inSet(set map[string]bool, p []string) bool {
	for _, s := range p {
		if set[strings.ToLower(strings.TrimSpace(s))] {
			return true
		}
	}
	return false
}

// ticketOf is the ID when rel is a ticket note,
// tickets/<source>/<ID>/<ID>.md (or an old vault's tickets/<ID>/<ID>.md).
func ticketOf(rel string) (string, bool) { return tnote.RelNote(rel) }

// inTicketDir reports whether rel is inside ticket id's folder, the one
// the vault has (tickets/<source>/<ID>/), not another folder of that name.
func inTicketDir(vault, id, rel string) bool {
	return strings.HasPrefix(rel, tnote.RelDir(vault, id)+"/")
}

func idOf(item string) string {
	item = strings.TrimSpace(item)
	item = strings.TrimSuffix(strings.TrimPrefix(item, "[["), "]]")
	if i := strings.IndexAny(item, "|#"); i >= 0 {
		item = item[:i]
	}
	return strings.TrimSuffix(path.Base(item), ".md")
}

// covers is the IDs the workspace ticket covers, when it is an epic.
func covers(vault, id string) map[string]bool {
	data, err := os.ReadFile(tnote.TicketPath(vault, id))
	if err != nil {
		return nil
	}
	if t, _ := Field(data, "ticket-type"); unquote(t) != "epic" {
		return nil
	}
	out := map[string]bool{}
	for _, c := range ListField(data, "covers") {
		out[idOf(c)] = true
	}
	return out
}

const whoWrites = `(AGENTS.md, "Who writes what" 3); /tickets:save passes --save`

// Check applies the write rules to a write of rel (vault-relative, with
// slashes) touching target. A nil result means it is allowed. Check reads
// the ticket note from disk itself, while Apply passes the src it already
// read; both reach the same manual decision (isManual) from those bytes.
func Check(c Ctx, rel string, t Target) error {
	var src []byte
	if _, ok := ticketOf(path.Clean(filepath.ToSlash(rel))); ok {
		src, _ = os.ReadFile(filepath.Join(c.Vault, filepath.FromSlash(path.Clean(filepath.ToSlash(rel)))))
	}
	_, err := check(c, rel, src, t)
	return err
}

// sourceOf is the note's frontmatter source, unquoted and trimmed ("" when
// the note has none).
func sourceOf(src []byte) string {
	v, _ := Field(src, "source")
	return strings.TrimSpace(unquote(v))
}

// isManual says whether the note, as it is on disk before a write, is a
// manual ticket: its source is manual. Anything else fails closed.
// A note with more than one source key, or frontmatter that does not parse,
// is not manual.
func isManual(src []byte) bool {
	n := 0
	for _, kv := range Fields(src) {
		if kv.Key == "source" {
			n++
		}
	}
	if n != 1 {
		return false
	}
	if _, err := FieldsJSON(src); err != nil {
		return false
	}
	return strings.EqualFold(sourceOf(src), "manual")
}

func check(c Ctx, rel string, src []byte, t Target) (coveredOnly bool, err error) {
	rel = path.Clean(filepath.ToSlash(rel))
	if rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
		return false, refuse("refusing to write %s: outside the vault", rel)
	}
	tid, isTicket := ticketOf(rel)
	switch {
	case c.Save:
		for _, p := range strings.Split(rel, "/") {
			if strings.HasPrefix(p, ".") {
				return false, refuse("refusing to write %s: --save never writes dot-directories or dot-files", rel)
			}
		}
		if top := strings.SplitN(rel, "/", 2)[0]; top == "raw" || top == "templates" || top == "archive" {
			return false, refuse("refusing to write %s: --save never writes raw/, templates/ or archive/", rel)
		}
	case c.Workspace != "":
		if !inTicketDir(c.Vault, c.Workspace, rel) {
			if isTicket && tid != c.Workspace && covers(c.Vault, c.Workspace)[tid] && inTicketDir(c.Vault, tid, rel) {
				coveredOnly = true
				break
			}
			return false, refuse("refusing to write %s: a ticket's agents write only inside %s/ until /tickets:save %s", rel, tnote.RelDir(c.Vault, c.Workspace), whoWrites)
		}
	default:
		if !strings.HasPrefix(rel, "inbox/") {
			return false, refuse("refusing to write %s: outside a ticket workspace agents write only inside inbox/ until /tickets:save %s", rel, whoWrites)
		}
	}
	if !isTicket {
		return coveredOnly, nil
	}
	if t.Field == "" && len(t.Section) == 0 {
		return false, refuse("refusing to write %s: the write names no section or field, so the rules can't check it", rel)
	}
	if t.Field != "" {
		if coveredOnly {
			if strings.ToLower(t.Field) != "status" {
				return false, refuse("refusing to write field %q of %s: an epic writes only the work sections and status of the tickets it covers", t.Field, rel)
			}
		} else if protectedField(t.Field) {
			return false, refuse("refusing to write field %q of %s: the identity, source and ignore fields of a ticket note are never written by agents (AGENTS.md, \"Who writes what\")", t.Field, rel)
		}
	}
	if len(t.Section) > 0 {
		if inSet(contentSections, t.Section) {
			if coveredOnly {
				return false, refuse("refusing to write section %q of %s: an epic writes only the work sections and status of the tickets it covers; the covered ticket's own workspace edits its Description, Acceptance criteria and Context / links (AGENTS.md, \"Who writes what\")", strings.Join(t.Section, " > "), rel)
			}
			if !isManual(src) {
				v := sourceOf(src)
				if v == "" {
					v = "none"
				}
				return false, refuse("refusing to write section %q of %s: the Description, Acceptance criteria and Context / links of a ticket note whose source is not manual (source: %s) are never written by agents (AGENTS.md, \"Who writes what\")", strings.Join(t.Section, " > "), rel, v)
			}
		}
		if coveredOnly && !inSet(workSections, t.Section) {
			return false, refuse("refusing to write section %q of %s: an epic writes only the work sections and status of the tickets it covers", strings.Join(t.Section, " > "), rel)
		}
	}
	return coveredOnly, nil
}

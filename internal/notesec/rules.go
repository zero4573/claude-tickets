package notesec

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
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

// ticketOf is the ID when rel is a ticket note, tickets/<ID>/<ID>.md.
func ticketOf(rel string) (string, bool) {
	p := strings.Split(rel, "/")
	if len(p) == 3 && p[0] == "tickets" && p[2] == p[1]+".md" {
		return p[1], true
	}
	return "", false
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
	data, err := os.ReadFile(filepath.Join(vault, "tickets", id, id+".md"))
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
// slashes) touching target. A nil result means it is allowed.
func Check(c Ctx, rel string, t Target) error {
	rel = path.Clean(filepath.ToSlash(rel))
	if rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
		return refuse("refusing to write %s: outside the vault", rel)
	}
	tid, isTicket := ticketOf(rel)
	coveredOnly := false // a covered ticket's note: work sections and status only
	switch {
	case c.Save:
		for _, p := range strings.Split(rel, "/") {
			if strings.HasPrefix(p, ".") {
				return refuse("refusing to write %s: --save never writes dot-directories or dot-files", rel)
			}
		}
		if top := strings.SplitN(rel, "/", 2)[0]; top == "raw" || top == "templates" || top == "archive" {
			return refuse("refusing to write %s: --save never writes raw/, templates/ or archive/", rel)
		}
	case c.Workspace != "":
		if !strings.HasPrefix(rel, "tickets/"+c.Workspace+"/") {
			if isTicket && tid != c.Workspace && covers(c.Vault, c.Workspace)[tid] {
				coveredOnly = true
				break
			}
			return refuse("refusing to write %s: a ticket's agents write only inside tickets/%s/ until /tickets:save %s", rel, c.Workspace, whoWrites)
		}
	default:
		if !strings.HasPrefix(rel, "inbox/") {
			return refuse("refusing to write %s: outside a ticket workspace agents write only inside inbox/ until /tickets:save %s", rel, whoWrites)
		}
	}
	if !isTicket {
		return nil
	}
	if t.Field != "" {
		if coveredOnly {
			if strings.ToLower(t.Field) != "status" {
				return refuse("refusing to write field %q of %s: an epic writes only the work sections and status of the tickets it covers", t.Field, rel)
			}
		} else if protectedField(t.Field) {
			return refuse("refusing to write field %q of %s: the identity, source and ignore fields of a ticket note are never written by agents (AGENTS.md, \"Who writes what\")", t.Field, rel)
		}
	}
	if len(t.Section) > 0 {
		if inSet(contentSections, t.Section) {
			return refuse("refusing to write section %q of %s: the Description, Acceptance criteria and Context / links of a ticket note are never written by agents (AGENTS.md, \"Who writes what\")", strings.Join(t.Section, " > "), rel)
		}
		if coveredOnly && !inSet(workSections, t.Section) {
			return refuse("refusing to write section %q of %s: an epic writes only the work sections and status of the tickets it covers", strings.Join(t.Section, " > "), rel)
		}
	}
	return nil
}

// Package clean is ct clean: it finds the vault's finished tickets, checks
// that nothing of theirs would be lost (running sessions, uncommitted or
// unpushed work, an unsaved session), and removes them: a synced ticket's
// folder goes to the vault's .trash/ (links to it become links to its
// remote page), a manual ticket's folder to archive/tickets/ (links keep
// resolving).
package clean

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zero4573/claude-tickets/internal/links"
	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

// Action is what ct clean does with a ticket.
type Action int

const (
	Skip    Action = iota
	Trash          // to <vault>/.trash/<ID>/, links rewritten to its URL
	Purge          // deleted, links rewritten to its URL
	Archive        // to archive/tickets/<ID>/, links unchanged
	Keep           // left where it is (--keep-manual)
)

// Options is a run's flags and where it works.
type Options struct {
	Vault, WorkRoot, TmuxSession string
	IDs                          []string // only these (default: every ticket)
	IncludeDone, KeepManual      bool
	Purge, Save                  bool
	Days                         int
	Now                          time.Time
	// Running tells a workspace's session is running (default:
	// workspace.Running with TmuxSession)
	Running func(dir string) bool
}

// Item is one ticket's plan.
type Item struct {
	ID, Status string
	Action     Action
	// Reason is why it's skipped (Action Skip)
	Reason string
	// Task is the follow-up task for a skip the user can act on
	Task     string
	Explicit bool // named on the command line
	// Save: run /tickets:save first, in SaveIn's workspace
	Save   bool
	SaveIn string
	URL    string
	Lead   string   // the lead it's covered by
	Covers []string // a lead's covered tickets
}

// Acts reports whether the item changes anything (a save counts).
func (it Item) Acts() bool { return it.Save || it.Action != Skip && it.Action != Keep }

func (o Options) running(dir string) bool {
	if o.Running != nil {
		return o.Running(dir)
	}
	return workspace.Running(dir, o.TmuxSession)
}

func (o Options) wsDir(id string) string { return filepath.Join(o.WorkRoot, id) }

// hasWorkspace reports whether a workspace exists (it holds workspace.json).
func hasWorkspace(dir string) bool {
	_, err := os.Stat(workspace.File(dir))
	return err == nil
}

func finished(status string) bool { return status == "done" || status == "closed" }

// linkTarget strips a frontmatter link's brackets and quotes: [[PROJ-1]] -> PROJ-1.
func linkTarget(s string) string {
	return strings.TrimSpace(strings.NewReplacer("[", "", "]", "", `"`, "", "'", "").Replace(s))
}

// coversOf is a lead ticket's covered tickets (none unless ticket-type is
// epic).
func coversOf(file string) []string {
	f := note.Fields(file)
	if f["ticket-type"] != "epic" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(f["covers"], ",") {
		if id := linkTarget(item); note.ValidKey(id) {
			out = append(out, id)
		}
	}
	return out
}

// usableURL is an absolute http(s) URL.
func usableURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Plan decides, for every ticket (or o.IDs), what ct clean does, reading
// only. Tickets that aren't finished are left out unless named.
func Plan(o Options) []Item {
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	ids := o.IDs
	explicit := len(ids) > 0
	if !explicit {
		for _, t := range note.List(o.Vault, true) {
			ids = append(ids, t.ID)
		}
	}
	var items []Item
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if it, ok := planOne(o, id, explicit); ok {
			items = append(items, it)
		}
	}
	// A lead goes only with (or after) every ticket it covers: their notes
	// still get its /tickets:save, and their work is in its workspace
	for changed := true; changed; {
		changed = false
		planned := map[string]bool{}
		for _, it := range items {
			if it.Action != Skip {
				planned[it.ID] = true
			}
		}
		for i, it := range items {
			if it.Action == Skip || len(it.Covers) == 0 {
				continue
			}
			var waiting []string
			for _, c := range it.Covers {
				if _, err := os.Stat(note.TicketPath(o.Vault, c)); err == nil && !planned[c] {
					waiting = append(waiting, c)
				}
			}
			if len(waiting) > 0 {
				items[i] = skip(it, "waiting on covered tickets: "+strings.Join(waiting, ", "), "")
				changed = true
			}
		}
	}
	return items
}

func skip(it Item, reason, task string) Item {
	it.Action, it.Reason, it.Task, it.Save = Skip, reason, task, false
	return it
}

// planOne plans one ticket on its own (ok false: not a candidate, and not
// named).
func planOne(o Options, id string, explicit bool) (Item, bool) {
	it := Item{ID: id, Explicit: explicit}
	file := note.TicketPath(o.Vault, id)
	if _, err := os.Stat(file); !note.ValidKey(id) || err != nil {
		return skip(it, "not a ticket in the vault", ""), explicit
	}
	fm := note.Frontmatter(file)
	it.Status = fm["status"]
	synced := note.Synced(fm)
	switch {
	case it.Status == "closed", it.Status == "done" && o.IncludeDone:
	case it.Status == "done":
		return skip(it, "done, not closed (--include-done to clean it)", ""), explicit
	default:
		st := it.Status
		if st == "" {
			st = "none"
		}
		return skip(it, "not finished (status "+st+")", ""), explicit
	}
	if it.Status == "done" && synced {
		return skip(it, "done synced ticket (ct sync would re-create it)", ""), true
	}
	if o.Days > 0 {
		if age := ageDays(file, fm["updated"], o.Now); age < o.Days {
			return skip(it, fmt.Sprintf("updated %d day(s) ago (--days %d)", age, o.Days), ""), true
		}
	}
	it.URL = fm["source-url"]
	remote := synced && usableURL(it.URL)
	// --keep-manual leaves it as it is: nothing to check, nothing to save
	if !remote && o.KeepManual {
		it.Action, it.Covers = Keep, coversOf(file)
		return it, true
	}
	dirs := []string{o.wsDir(id)}
	if lead := linkTarget(fm["covered-by"]); lead != "" {
		it.Lead = lead
		if st := note.Get(note.TicketPath(o.Vault, lead), "status"); st != "" && !finished(st) {
			return skip(it, "lead "+lead+" still open", ""), true
		}
		dirs = append(dirs, o.wsDir(lead))
	}
	it.Covers = coversOf(file)
	if reason, task := safety(o, id, it.Status, dirs); reason != "" {
		return skip(it, reason, task), true
	}

	// The workspace it was worked in: its own, else its lead's
	saveIn := ""
	for _, d := range dirs {
		if hasWorkspace(d) {
			saveIn = d
			break
		}
	}
	if state, why := note.SaveStateOf(o.Vault, saveIn, id); state == note.Unsaved {
		if !o.Save || saveIn == "" {
			return skip(it, "needs /tickets:save ("+why+")", fmt.Sprintf(
				"[[%s]] (%s) wasn't saved (%s): ct start %s, run /tickets:save, then ct clean %s (or ct clean --save %s)",
				id, it.Status, why, filepath.Base(firstNonEmpty(saveIn, o.wsDir(id))), id, id)), true
		}
		if slug := nullVersion(saveIn); slug != "" {
			return skip(it, "needs /tickets:save, and "+slug+" has no target version (a headless save can't ask)", fmt.Sprintf(
				"[[%s]] (%s) wasn't saved, and its workspace has no target version for %s: ct start %s, run /tickets:save, then ct clean %s",
				id, it.Status, slug, filepath.Base(saveIn), id)), true
		}
		it.Save, it.SaveIn = true, filepath.Base(saveIn)
	}

	switch {
	case remote && o.Purge:
		it.Action = Purge
	case remote:
		it.Action = Trash
	default:
		it.Action = Archive
	}
	return it, true
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// nullVersion is the slug of a workspace repo without a target version
// (/tickets:save would ask for it).
func nullVersion(dir string) string {
	info, _ := workspace.Read(dir)
	for _, r := range info.Repos {
		if r.TargetVersion == nil || *r.TargetVersion == "" {
			return r.Slug
		}
	}
	return ""
}

// ageDays is how many days ago a note was updated: its updated: date
// (yyyy-MM-dd), else the file's modification time.
func ageDays(file, updated string, now time.Time) int {
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(updated), time.Local)
	if err != nil {
		st, serr := os.Stat(file)
		if serr != nil {
			return 0
		}
		t = st.ModTime()
	}
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	y, m, d = t.Date()
	// (a date in the future counts as today)
	return max(0, int(today.Sub(time.Date(y, m, d, 0, 0, 0, 0, time.Local)).Hours()/24))
}

// Order is the order tickets are removed in: covered tickets first (a
// lead's files their notes embed then count as theirs), then by ID.
func Order(items []Item) []Item {
	out := append([]Item(nil), items...)
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Lead != "") != (out[j].Lead != "") {
			return out[i].Lead != ""
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// SyncOwned is a links Skip/Exempt test: a place ct sync owns in a synced
// ticket's note (a sync-owned frontmatter field, the source block).
func SyncOwned() func(links.Place) bool {
	cache := map[string]map[string]string{}
	return func(p links.Place) bool {
		if !note.IsTicketNote(p.File) {
			return false
		}
		fm, ok := cache[p.File]
		if !ok {
			fm = note.Frontmatter(p.File)
			cache[p.File] = fm
		}
		field := ""
		if p.Frontmatter {
			field = p.Field
		}
		return note.SyncOwned(fm, field, p.SourceBlock)
	}
}

package clean

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/zero4573/claude-tickets/internal/followup"
	"github.com/zero4573/claude-tickets/internal/fsx"
	"github.com/zero4573/claude-tickets/internal/links"
	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/vaultlock"
)

const lockOwner = "ct-clean"

// Env is what a run does outside the vault, and where it reports.
type Env struct {
	Out io.Writer
	// Save runs /tickets:save headless in workspace id's folder; it
	// returns its log
	Save func(id string) (log string, err error)
	// RemoveWorkspace removes <workRoot>/<id> as ct ws rm does
	RemoveWorkspace func(id string) error
}

// Result is how a run went.
type Result struct {
	Cleaned  int
	Failed   bool   // a step failed, or a named ticket was skipped
	FollowUp string // the follow-up note written, if any
}

func rel(vault, p string) string {
	if r, err := filepath.Rel(vault, p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}

// Describe is an item's plan line; for a ticket going to the trash it
// counts the links it would rewrite and the files it would keep.
func Describe(o Options, items []Item, it Item) string {
	head := it.ID
	if it.Status != "" {
		head += " (" + it.Status + ")"
	}
	what := ""
	switch it.Action {
	case Skip:
		return head + ": skip: " + it.Reason
	case Keep:
		return head + ": keep (manual)"
	case Archive:
		what = "archive to " + rel(o.Vault, note.ArchiveDir(o.Vault, it.ID))
	case Trash, Purge:
		dir := note.TicketDir(o.Vault, it.ID)
		kept := links.Referenced(o.Vault, dir, gone(o, items, it.ID))
		n, notes, _ := links.RewriteToURL(o.Vault, note.TicketPath(o.Vault, it.ID), it.ID, it.URL, []string{dir},
			links.RewriteOpts{DryRun: true, Skip: SyncOwned()})
		what = "move to .trash/" + it.ID
		if it.Action == Purge {
			what = "delete"
		}
		what += fmt.Sprintf("; rewrite %d link(s) in %d note(s) to %s", n, notes, it.URL)
		if len(kept) > 0 {
			what += fmt.Sprintf("; keep %d file(s) in %s", len(kept), rel(o.Vault, note.ArchiveDir(o.Vault, it.ID)))
		}
	}
	if it.Save {
		what = "run /tickets:save (in " + it.SaveIn + "), then " + what
	}
	if hasWorkspace(o.wsDir(it.ID)) {
		what = "remove the workspace; " + what
	}
	return head + ": " + what
}

// gone is the folders of the other tickets this run trashes or deletes:
// their links to a ticket's files go with them.
func gone(o Options, items []Item, except string) []string {
	var out []string
	for _, it := range items {
		if it.ID != except && (it.Action == Trash || it.Action == Purge) {
			out = append(out, note.TicketDir(o.Vault, it.ID))
		}
	}
	return out
}

// Run carries out a plan (from Plan, confirmed): the saves first, then each
// ticket under the vault lock, covered tickets before their leads; then it
// checks the links of the notes it changed and leaves follow-up tasks for
// what it skipped.
func Run(o Options, items []Item, env Env) Result {
	out := env.Out
	var res Result
	var tasks []string
	say := func(format string, a ...any) { fmt.Fprintf(out, "ct clean: "+format+"\n", a...) }

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	stopped := func() bool {
		select {
		case <-stop:
			say("interrupted: stopping before the next ticket (run ct clean again to finish)")
			res.Failed = true
			return true
		default:
			return false
		}
	}

	// Saves: without the lock, which /tickets:save takes itself
	failedSave := map[string]string{}
	saved := map[string]bool{}
	start := time.Now().Truncate(time.Second)
	for _, it := range items {
		if !it.Save || saved[it.SaveIn] || failedSave[it.SaveIn] != "" {
			continue
		}
		if stopped() {
			return res
		}
		log, err := env.Save(it.SaveIn)
		if err != nil {
			failedSave[it.SaveIn] = fmt.Sprintf("/tickets:save failed (%v); see %s", err, log)
		} else {
			saved[it.SaveIn] = true
		}
	}
	if len(saved)+len(failedSave) > 0 {
		// Re-plan (save state, status, safety) and keep to what was confirmed
		plain := o
		plain.Save = false
		again := map[string]Item{}
		for _, it := range Plan(plain) {
			again[it.ID] = it
		}
		for i, it := range items {
			if !it.Save {
				continue
			}
			nit, ok := again[it.ID]
			switch {
			case failedSave[it.SaveIn] != "":
				nit = skip(it, failedSave[it.SaveIn], fmt.Sprintf("[[%s]] (%s): ct clean --save couldn't save it: %s. Run /tickets:save in ct start %s, then ct clean %s",
					it.ID, it.Status, failedSave[it.SaveIn], it.SaveIn, it.ID))
			case !ok:
				nit = skip(it, "no longer a candidate after its save", "")
			case !savedSince(o.Vault, it.ID, start) && nit.Action != Skip:
				nit = skip(it, "/tickets:save ran but didn't mark it saved", fmt.Sprintf(
					"[[%s]] (%s): ct clean --save ran /tickets:save, but it didn't set saved:. Check it in ct start %s, then ct clean %s",
					it.ID, it.Status, it.SaveIn, it.ID))
			}
			nit.Explicit = it.Explicit
			if nit.Action == Skip {
				say("%s: skipped: %s", it.ID, nit.Reason)
			}
			items[i] = nit
		}
		// A lead whose covered ticket failed to save waits again
		for i, it := range items {
			if it.Action == Skip || len(it.Covers) == 0 {
				continue
			}
			for _, c := range it.Covers {
				for _, other := range items {
					if other.ID == c && other.Action == Skip {
						items[i] = skip(it, "waiting on covered tickets: "+c, "")
					}
				}
			}
		}
	}

	removed := map[string]bool{}
	var changed []string
	for _, it := range Order(items) {
		// (skips were shown with the plan)
		if it.Action == Skip {
			if it.Task != "" {
				tasks = append(tasks, it.Task)
			}
			res.Failed = res.Failed || it.Explicit
			continue
		}
		if !it.Acts() {
			continue
		}
		if stopped() {
			break
		}
		msg, more, err := one(o, env, items, it, &changed)
		tasks = append(tasks, more...)
		if err != nil {
			say("%s: %v", it.ID, err)
			res.Failed = true
			continue
		}
		removed[strings.ToLower(it.ID)] = true
		res.Cleaned++
		say("%s: %s", it.ID, msg)
	}

	// Nothing may point at a removed ticket now (sync-owned places aside)
	var still []string
	for _, f := range changed {
		if _, err := os.Stat(f); err != nil {
			continue
		}
		still = append(still, f)
	}
	if len(still) > 0 {
		owned := SyncOwned()
		for _, p := range links.Problems(o.Vault, still) {
			base := strings.ToLower(p.Target[strings.LastIndex(p.Target, "/")+1:])
			if len(p.Found) == 0 && removed[base] && !owned(p.Place) {
				say("warning: %s:%d still links [[%s]]", rel(o.Vault, p.File), p.Line, p.Target)
			}
		}
	}

	if len(tasks) > 0 {
		n, err := followup.Write(o.Vault, "ct-clean", "ct clean", tasks)
		if err != nil {
			say("couldn't write the follow-up note: %v", err)
		} else if n != "" {
			res.FollowUp = n
			say("follow-ups in %s", n)
		}
	}
	return res
}

// savedSince reports whether a ticket's saved: marker is from start on.
func savedSince(vault, id string, start time.Time) bool {
	t, err := time.ParseInLocation("2006-01-02T15:04:05", note.Get(note.TicketPath(vault, id), "saved"), time.Local)
	return err == nil && !t.Before(start)
}

// one removes a ticket, under the vault lock: its workspace, then (to the
// trash, or deleted) its referenced files to the archive, the links to it
// to its URL, its folder; or (archive) its folder to the archive. tasks are
// follow-ups for links it couldn't rewrite.
func one(o Options, env Env, items []Item, it Item, changed *[]string) (msg string, tasks []string, err error) {
	if err := vaultlock.Acquire(o.Vault, lockOwner); err != nil {
		return "", nil, err
	}
	defer func() {
		if rerr := vaultlock.Release(o.Vault, lockOwner); rerr != nil && err == nil {
			err = rerr
		}
	}()
	// Things may have changed since the plan (a session started)
	now, _ := planOne(withoutSave(o), it.ID, true)
	if now.Action != it.Action {
		reason := now.Reason
		if reason == "" {
			reason = "its plan changed"
		}
		return "", nil, errors.New("skipped: " + reason)
	}

	var parts []string
	if fsx.IsDir(o.wsDir(it.ID)) {
		if err := env.RemoveWorkspace(it.ID); err != nil {
			return "", nil, fmt.Errorf("workspace not removed (%v); nothing else changed", err)
		}
		parts = append(parts, "removed its workspace")
	}

	dir := note.TicketDir(o.Vault, it.ID)
	archive := note.ArchiveDir(o.Vault, it.ID)
	if it.Action == Archive {
		moved, _, err := links.MoveTree(o.Vault, dir, archive, nil, it.ID+".md")
		if err != nil {
			return "", nil, err
		}
		return strings.Join(append(parts, fmt.Sprintf("archived %d file(s) to %s", moved, rel(o.Vault, archive))), "; "), nil, nil
	}

	kept := links.Referenced(o.Vault, dir, gone(o, items, it.ID))
	if len(kept) > 0 {
		if _, _, err := links.MoveTree(o.Vault, dir, archive, kept, ""); err != nil {
			return "", nil, err
		}
	}
	n, notes, err := links.RewriteToURL(o.Vault, note.TicketPath(o.Vault, it.ID), it.ID, it.URL, []string{dir}, links.RewriteOpts{
		Skip: SyncOwned(),
		NotRewritten: func(p links.Place) {
			tasks = append(tasks, fmt.Sprintf("%s:%d still links %s (removed by ct clean) in a frontmatter form it can't rewrite: make it \"[%s](%s)\"",
				rel(o.Vault, p.File), p.Line, it.ID, it.ID, it.URL))
		},
		Changed: func(f string) { *changed = append(*changed, f) },
	})
	if err != nil {
		return "", tasks, err
	}
	where := ""
	if it.Action == Purge {
		if err := fsx.RemoveAll(dir); err != nil {
			return "", tasks, err
		}
		where = "deleted"
	} else {
		trash := filepath.Join(o.Vault, ".trash", it.ID)
		if _, err := os.Lstat(trash); err == nil {
			trash += "-" + time.Now().Format("20060102150405")
		}
		if err := os.MkdirAll(filepath.Dir(trash), 0o755); err != nil {
			return "", tasks, err
		}
		if err := os.Rename(dir, trash); err != nil {
			return "", tasks, err
		}
		where = "moved to " + rel(o.Vault, trash)
	}
	parts = append(parts, where, fmt.Sprintf("rewrote %d link(s) in %d note(s)", n, notes))
	if len(kept) > 0 {
		parts = append(parts, fmt.Sprintf("kept %d file(s) in %s", len(kept), rel(o.Vault, archive)))
	}
	return strings.Join(parts, "; "), tasks, nil
}

func withoutSave(o Options) Options {
	o.Save = false
	return o
}

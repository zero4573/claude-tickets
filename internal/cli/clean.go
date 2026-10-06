package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/clean"
	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/prompt"
	"github.com/zero4573/claude-tickets/internal/session"
	"github.com/zero4573/claude-tickets/internal/vault"
)

func finishedTickets(ctx vault.Context) []note.Ticket {
	var out []note.Ticket
	for _, t := range note.List(ctx.Vault, true) {
		if st := note.Get(note.TicketPath(ctx.Vault, t.ID), "status"); st == "done" || st == "closed" {
			out = append(out, t)
		}
	}
	return out
}

func cleanCmd() *cobra.Command {
	var o clean.Options
	var dry, yes bool
	cmd := &cobra.Command{
		Use:   "clean [<ID>...] [-n|--dry-run] [-y|--yes] [--save] [--include-done] [--keep-manual] [--purge] [--days N]",
		Short: "Clean up closed tickets: their workspace, their folder, the links to them",
		Long: `Cleans up the current vault's closed tickets (or only the ones named), one
at a time:

- a synced ticket (one with a source-url) goes to the vault's
  .trash/<ID>/ (--purge deletes it instead), and every link to it
  ([[ID]], [[ID|alias]], [[ID#heading]], ![[ID]], path forms) becomes a
  link to its remote page, [ID](<source-url>). Files of its folder that
  other notes link or embed (a log, an image) move to archive/tickets/<ID>/
  first, so those links keep working;
- a manual ticket (or a synced one without a source-url) moves whole to
  archive/tickets/<ID>/: links to it keep resolving, and ct new never reuses
  its number. --keep-manual leaves them where they are.

Its workspace goes first (as ct ws rm <ID>). Links in a synced ticket's
sync-owned parts (parent, children, blocked-by, blocks, related, covers,
covered-by, source-* fields, and the source block) are left as they are:
ct sync rewrites those.

A ticket is skipped, with the reason, when:
- it isn't closed (done ones too with --include-done, but never synced
  done ones: ct sync would re-create them while they're open at the source);
- --days N and its note was updated less than N days ago;
- its session is running, or its workspace has uncommitted changes, or
  commits on no remote (or a stash);
- its work wasn't saved: no saved: marker (written by /tickets:save) or
  summary block, and it has hand-off files, logs or commits, or kb-drafts
  not yet promoted. --save runs /tickets:save headless in its workspace
  first (refused when a repo there has no target version: the save would
  have to ask), logged to ` + logPath("clean-save-<ID>") + `;
- it's a lead (ticket-type: epic) with covered tickets that aren't going in
  this run, or a covered ticket whose lead is still open (its workspace is
  checked too).

It prints the plan, one line per ticket, then asks; without a terminal it
needs --yes. --dry-run only prints the plan and changes nothing. Each ticket
is cleaned under the vault lock (ct vault lock), so it never interleaves with
a /tickets:save. Skipped tickets you can act on become tasks in a follow-up
note, inbox/<date>-ct-clean-follow-ups.md. Run it on the host: inside a
container it can't see running sessions. Exits 1 if a named ticket was
skipped or a step failed.`,
		ValidArgsFunction: completeIDs(finishedTickets),
		RunE: func(cmd *cobra.Command, args []string) error {
			if o.Days < 0 {
				return errors.New("clean: --days must be 0 or more")
			}
			// It tells running sessions apart by the vault's tmux session; inside
			// a container it would see none
			if gitx.InContainer() {
				return errors.New("clean: run it on the host, not inside a container")
			}
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			o.Vault, o.WorkRoot, o.TmuxSession, o.IDs = ctx.Vault, ctx.WorkRoot, ctx.TmuxSession, args
			return runClean(ctx, o, dry, yes)
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&dry, "dry-run", "n", false, "only print the plan")
	f.BoolVarP(&yes, "yes", "y", false, "don't ask (needed without a terminal)")
	f.BoolVar(&o.Save, "save", false, "run /tickets:save headless for worked, unsaved tickets first")
	f.BoolVar(&o.IncludeDone, "include-done", false, "done tickets too (never synced ones)")
	f.BoolVar(&o.KeepManual, "keep-manual", false, "leave manual tickets where they are instead of archiving them")
	f.BoolVar(&o.Purge, "purge", false, "delete synced tickets' folders instead of moving them to .trash/")
	f.IntVar(&o.Days, "days", 0, "only tickets whose note was updated at least N days ago")
	return cmd
}

func runClean(ctx vault.Context, o clean.Options, dry, yes bool) error {
	items := clean.Plan(o)
	acts, named := 0, false
	for _, it := range items {
		fmt.Println("ct clean: " + clean.Describe(o, items, it))
		if it.Acts() {
			acts++
		}
		named = named || it.Action == clean.Skip && it.Explicit
	}
	if dry {
		if named {
			return exitError(1)
		}
		return nil
	}
	if acts == 0 {
		fmt.Println("ct clean: nothing to clean")
	} else if !yes {
		if !prompt.Interactive() {
			return exitWith(2, errors.New("clean: not a terminal; review with --dry-run, then re-run with --yes"))
		}
		if !prompt.YesNo(fmt.Sprintf("Clean %d ticket(s) as above?", acts), "n") {
			return exitError(1)
		}
	}
	res := clean.Run(o, items, clean.Env{
		Out:             os.Stdout,
		Save:            func(id string) (string, error) { return headlessSave(ctx, id) },
		RemoveWorkspace: func(id string) error { return wsRm(ctx, id, false) },
	})
	if res.Failed {
		return exitError(1)
	}
	return nil
}

// headlessSave runs /tickets:save in a ticket's workspace, with no one to
// answer questions.
func headlessSave(ctx vault.Context, id string) (string, error) {
	log := config.StateLog("clean-save-" + id)
	dir, argv, err := prepareTicketSession(ctx, id, sessionDirs(ctx))
	if err != nil {
		return log, err
	}
	argv = append(argv, "--permission-mode", "auto", "--disallowedTools", "AskUserQuestion",
		"-p", "/tickets:save", "--output-format", "stream-json", "--verbose")
	fmt.Printf("ct clean: %s: running /tickets:save in %s (log: %s)\n", id, config.TildePath(dir), config.TildePath(log))
	return config.TildePath(log), session.HeadlessIn(dir, log, argv)
}

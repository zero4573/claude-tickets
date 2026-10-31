package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zero4573/tix/internal/note"
	"github.com/zero4573/tix/internal/spec"
	"github.com/zero4573/tix/internal/ticketconv"
	"github.com/zero4573/tix/internal/vault"
	"github.com/zero4573/tix/internal/workspace"
)

func ticketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ticket",
		Short: "Work on a ticket's note and place in the vault",
	}
	cmd.AddCommand(ticketConvertCmd())
	cmd.AddCommand(ticketPublishCmd())
	cmd.AddCommand(ticketStubCmd())
	return cmd
}

func ticketConvertCmd() *cobra.Command {
	var o ticketconv.Options
	cmd := &cobra.Command{
		Use:   "convert <MAN-n> --to <source> [--project PROJ] [--type T] [--dry-run]",
		Short: "File a manual ticket at a source and move its vault folder and workspace to the new ID",
		Long: `Files the manual ticket at a synced source (e.g. jira) and renames it to the
new key: the issue gets the note's summary, Description and Acceptance
criteria, you as assignee and, when a synced epic covers the ticket, that epic
as parent. Then tickets/manual/<MAN-n>/ moves to tickets/<source>/<KEY>/ (files
renamed, links across the vault following, the ID rewritten inside the folder,
the note turned into a synced one for tix sync to fill), and the workspace
<MAN-n> is renamed to <KEY> with its worktrees repaired. The branch keeps its
name. The session's conversation starts fresh.

Refused while the ticket's session runs, for a source that isn't an enabled
synced one, and for a ticket that isn't manual. --project falls back to a
"project" key on the source in tickets/.sources.json; --type to the type
mapped from the note's ticket-type (dev: Story, bug: Bug, chore and
investigation: Task, epic: Epic). --dry-run shows what would be filed. After a
failure past the create, run it again to finish (no second issue).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			o.Vault, o.WorkRoot, o.ID, o.Out = ctx.Vault, ctx.WorkRoot, args[0], cmd.OutOrStdout()
			if o.Source == "" {
				return errors.New("--to <source> is required (e.g. --to jira)")
			}
			if !o.DryRun {
				before, beforeErr := snapshotTickets(ctx.Vault)
				defer refreshAfterSync(ctx.Vault, before, beforeErr, "")
			}
			err = ticketconv.Run(o)
			var pe *ticketconv.PartialError
			if errors.As(err, &pe) {
				fmt.Fprintln(cmd.ErrOrStderr(), "tix: "+pe.Msg)
				return exitError(1)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&o.Source, "to", "", "the synced source to file the ticket at (e.g. jira)")
	cmd.Flags().StringVar(&o.Project, "project", "", "the source's project key (default: \"project\" on the source in .sources.json)")
	cmd.Flags().StringVar(&o.Type, "type", "", "the source's issue type (default: mapped from the note's ticket-type)")
	cmd.Flags().BoolVar(&o.DryRun, "dry-run", false, "show what would be filed and stop")
	return cmd
}

func ticketPublishCmd() *cobra.Command {
	var dry, allowEmpty bool
	var descFile string
	cmd := &cobra.Command{
		Use:   "publish <ID> [--dry-run] [--description-file <path>|-]",
		Short: "Print the ticket's description with its ## Spec change section rewritten",
		Long: `Builds the "## Spec change" section from "tix spec diff" for the ticket and
for each ticket it covers (the ones with a workspace) and prints the ticket's
description with it: the section replaced in place, or appended when the
description has none. Everything else is left byte for byte. --dry-run
prints the old and the new section instead.

The current description comes from --description-file (a path, or - for
standard input): the save skill fills it from the source's live issue right
before the edit, so nothing edited at the source since the last sync is lost.
Without it only --dry-run works, on the note's synced rendering, which holds
more than the description (custom field lines, a placeholder) and may be
stale: the output says so, and it must not be written to the source.

It writes nothing, locally or remotely: the save skill puts the printed
description into the ticket through the session's MCP tools.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			id := args[0]
			if !note.ValidKey(id) {
				return fmt.Errorf("invalid ticket ID %q", id)
			}
			if descFile == "" && !dry {
				return errors.New("--description-file <path>|- is required: publish rewrites the live description, which the note's synced copy only approximates (--dry-run alone may read the note)")
			}
			file := note.Find(ctx.Vault, id)
			if file == "" {
				return fmt.Errorf("no note for %s", id)
			}
			var desc string
			approx := descFile == ""
			if approx {
				var ok bool
				desc, ok, err = sourceDescription(file)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("%s has no source block with a description (tix sync writes it)", id)
				}
			} else {
				desc, err = readDescription(cmd.InOrStdin(), descFile)
				if err != nil {
					return err
				}
			}
			ids := []string{id}
			for _, c := range note.ListField(file, "covers") {
				if c = linkTarget(c); c != "" && c != id {
					ids = append(ids, c)
				}
			}
			var diffs []spec.Diff
			for i, tid := range ids {
				info, err := workspace.Read(filepath.Join(ctx.WorkRoot, tid))
				if err != nil {
					if i == 0 {
						return fmt.Errorf("no workspace for %s: %w", tid, err)
					}
					continue
				}
				for _, r := range info.Repos {
					d, _, err := specDiffRepo(ctx, tid, r)
					if err != nil {
						return fmt.Errorf("%s: %s: %w", tid, r.Slug, err)
					}
					diffs = append(diffs, d)
				}
			}
			body := spec.RenderChange(diffs...)
			if strings.TrimSpace(body) == "" && spec.HasChange(desc) && !allowEmpty {
				return errors.New("no spec change was found, but the description's ## Spec change section has one (a missing baseline? see tix spec promote); the section is left as is. If the change was really reverted, pass --allow-empty to clear it")
			}
			out := cmd.OutOrStdout()
			updated := spec.ReplaceChange(desc, body)
			if !dry {
				fmt.Fprint(out, updated)
				return nil
			}
			old, found := spec.CurrentChange(desc)
			if !found {
				old = "(none)\n"
			}
			now, _ := spec.CurrentChange(updated)
			if approx {
				fmt.Fprintln(out, "(approximate: read from the note's synced rendering, not the live description; pass --description-file for the real one)")
			}
			fmt.Fprintf(out, "--- old ---\n%s\n--- new ---\n%s", old, now)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dry, "dry-run", false, "print the old and the new spec change section, not the description")
	cmd.Flags().BoolVar(&allowEmpty, "allow-empty", false, "clear a non-empty spec change section when no change is found (the change was reverted)")
	cmd.Flags().StringVar(&descFile, "description-file", "", "the ticket's current description: a file, or - for standard input")
	return cmd
}

func ticketStubCmd() *cobra.Command {
	var o ticketconv.StubOptions
	cmd := &cobra.Command{
		Use:   "stub <ID> --source <source> --summary <text> --covered-by <lead>",
		Short: "Write the note of a ticket just filed at a source, covered by the lead ticket",
		Long: `Writes tickets/<source>/<ID>/<ID>.md for a ticket just filed at a synced
source, in the synced layout with an empty source block, its covered-by set to
the lead. For a manual lead it also adds the ticket to the lead's covers, which
tix sync keeps. For a lead that is itself synced (a source epic) the lead's
covers belong to the source and are left alone: only the child's covered-by is
written (and tix sync keeps it until the note's first sync). tix sync finds the
note by ID, fills the source block and fields, and keeps covered-by.

Refused when the ticket already has a note, when the lead has none, when the
lead's status is closed or done, and for a source that isn't an enabled synced
one. A lead that is not an epic (ticket-type) gets a warning. Nothing changes on a refusal. The
vault lock is held while the notes are written.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			o.Vault, o.ID, o.Out = ctx.Vault, args[0], cmd.OutOrStdout()
			if o.Source == "" {
				return errors.New("--source is required (e.g. --source jira)")
			}
			if o.CoveredBy == "" {
				return errors.New("--covered-by <lead ID> is required")
			}
			before, beforeErr := snapshotTickets(ctx.Vault)
			defer refreshAfterSync(ctx.Vault, before, beforeErr, "")
			return ticketconv.Stub(o)
		},
	}
	cmd.Flags().StringVar(&o.Source, "source", "", "the synced source the ticket was filed at (e.g. jira)")
	cmd.Flags().StringVar(&o.Summary, "summary", "", "the ticket's summary")
	cmd.Flags().StringVar(&o.CoveredBy, "covered-by", "", "the lead ticket that covers it")
	return cmd
}

// maxDescription caps what publish reads as a description.
const maxDescription = 4 << 20

// readDescription reads the description from path, or from in for "-".
func readDescription(in io.Reader, path string) (string, error) {
	r := in
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		r = f
	}
	b, err := io.ReadAll(io.LimitReader(r, maxDescription+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxDescription {
		return "", fmt.Errorf("description is larger than %d bytes", maxDescription)
	}
	return string(b), nil
}

// sourceDescription reads the text under "### Description" in a note's
// source block, up to "### Recent comments" or the block's end, without
// trailing blank lines (as tix sync wrote it).
func sourceDescription(file string) (string, bool, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", false, err
	}
	var out []string
	in, got := false, false
	for _, l := range strings.Split(note.Normalize(string(data)), "\n") {
		switch {
		case l == "<!-- source:start -->":
			in = true
		case l == "<!-- source:end -->":
			in = false
		case !in:
		case l == "### Description":
			got = true
		case l == "### Recent comments" && got:
			in = false
		case got:
			out = append(out, l)
		}
	}
	if !got {
		return "", false, nil
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return "", true, nil
	}
	return strings.Join(out, "\n") + "\n", true, nil
}

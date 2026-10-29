package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/ticketconv"
	"github.com/zero4573/claude-tickets/internal/vault"
)

func ticketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ticket",
		Short: "Work on a ticket's note and place in the vault",
	}
	cmd.AddCommand(ticketConvertCmd())
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
the note turned into a synced one for ct sync to fill), and the workspace
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
				fmt.Fprintln(cmd.ErrOrStderr(), "ct: "+pe.Msg)
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

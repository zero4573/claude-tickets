package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/zero4573/tix/internal/vault"
	"github.com/zero4573/tix/internal/workspace"
)

func wsRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh <ID>",
		Short: "Rewrite the workspace's CLAUDE.md from the vault (commit style, repos)",
		Long: `Rewrites <workRoot>/<ID>/CLAUDE.md exactly as tix start does, from the
vault's current state: the kickoff runs it once board.md names the ticket's
commit style. Other files tix start writes are left alone.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeWorkspaceID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			key := args[0]
			if err := requireKey("refresh", key); err != nil {
				return err
			}
			dir := filepath.Join(ctx.WorkRoot, key)
			if _, err := os.Stat(workspace.File(dir)); err != nil {
				return fmt.Errorf("refresh: %s has no workspace (%s)", key, dir)
			}
			if err := writeTicketClaudeMD(ctx, key, dir); err != nil {
				return err
			}
			fmt.Printf("tix ws refresh: %s rewritten\n", filepath.Join(dir, "CLAUDE.md"))
			return nil
		},
	}
}

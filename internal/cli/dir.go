package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zero4573/tix/internal/note"
	"github.com/zero4573/tix/internal/repo"
	"github.com/zero4573/tix/internal/vault"
)

func dirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dir work|tickets|repos",
		Short: "Print a directory of the current vault: its work, tickets or repo directory",
		Long: `Prints one of the current vault's directories, for cd "$(tix dir repos)":
  work     the ticket workspaces (workRoot)
  tickets  the vault's tickets folder
  repos    the main clones (projectsRoot), narrowed: while a level (the
           providers, then a provider's owners) holds a single folder, it
           goes into it; never into a repo itself`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"work", "tickets", "repos"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			switch args[0] {
			case "work":
				fmt.Println(ctx.WorkRoot)
			case "tickets":
				fmt.Println(note.TicketsDir(ctx.Vault))
			case "repos":
				fmt.Println(repo.Narrow(ctx.ProjectsRoot))
			default:
				return fmt.Errorf("dir: give work, tickets or repos, not '%s'", args[0])
			}
			return nil
		},
	}
}

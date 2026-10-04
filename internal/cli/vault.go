package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/prompt"
	"github.com/zero4573/claude-tickets/internal/vault"
)

func vaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "The current vault and where its tools work; vault setup",
		Long: `Without a subcommand: the vault every command acts on (the default vault,
else the only one), and where its workspaces, main clones and tmux session
are (its .workflow.json).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := vault.Current()
			if err != nil {
				return err
			}
			l := vault.LocationsOf(v)
			fmt.Printf("vault:        %s\n", config.TildePath(v))
			fmt.Printf("workspaces:   %s\n", config.TildePath(l.WorkRoot))
			fmt.Printf("main clones:  %s\n", config.TildePath(l.ProjectsRoot))
			fmt.Printf("tmux session: %s\n", l.TmuxSession)
			return nil
		},
	}
	cmd.AddCommand(
		vaultDefaultCmd(),
		scriptCmd("init [<vault>] [--defaults] [--allow-overlap]", "Set a vault up for the ticket workflow", "vault-init"),
		scriptCmd("configure [<vault>] [--section <name>]... [--missing] [--defaults] [--allow-overlap]",
			"A vault's settings: locations, ticket sources, container runtime", "vault-configure"),
		scriptCmd("lock acquire|release|status|run ...", "The lock /tickets:save holds while writing shared notes", "vault-lock"),
		scriptCmd("links check|move ...", "Check wikilinks; move a note without breaking links to it", "vault-links"),
	)
	return cmd
}

func vaultDefaultCmd() *cobra.Command {
	var pick, unset bool
	cmd := &cobra.Command{
		Use:   "default [<vault> | --pick | --unset]",
		Short: "Show or set the default vault, the one every command acts on",
		Long: `Shows or sets the default Obsidian vault: the vault every command acts on.
Switch it to work on another vault. Without one, the commands use the only
vault under ~/Documents/Obsidian. ct vault init always asks.

  ct vault default          show the default and where it comes from
  ct vault default <vault>  set it: a vault name under ~/Documents/Obsidian
                            (or a path)
  ct vault default --pick   choose it from the list of vaults
  ct vault default --unset  remove it

The default is stored in ~/.config/claude-tickets/default-vault. Sessions
already running keep the vault they started with.`,
		Args: cobra.MaximumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return vault.List(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case unset:
				if err := vault.UnsetDefault(); err != nil {
					return err
				}
				fmt.Println("ct: default vault removed")
				return nil
			case pick || len(args) == 1:
				var path string
				var err error
				if len(args) == 1 {
					path, err = vault.Resolve(args[0])
				} else {
					var name string
					if name, err = prompt.Pick("vault", vault.List()); err == nil {
						path, err = vault.Resolve(name)
					}
				}
				if err != nil {
					return err
				}
				saved, err := vault.SetDefault(path)
				if err != nil {
					return err
				}
				fmt.Printf("ct: default vault is now %s\n", saved)
				if config.LaunchVault != "" {
					fmt.Fprintf(os.Stderr, "ct: this shell has CLAUDE_TICKETS_VAULT=%s (from a ticket session), which wins over the default here; unset CLAUDE_TICKETS_VAULT\n", config.LaunchVault)
				}
				return nil
			}
			// Show it
			switch {
			case config.LaunchVault != "":
				fmt.Printf("default vault: %s (CLAUDE_TICKETS_VAULT: the vault this session was started with)\n", config.LaunchVault)
			case vault.SavedDefault() != "":
				fmt.Printf("default vault: %s (from %s)\n", vault.SavedDefault(), config.TildePath(config.DefaultVaultFile()))
			default:
				fmt.Println("no default vault set (ct vault default <vault> to set one)")
				return exitError(1)
			}
			if _, ok, err := vault.Default(); !ok {
				if err == nil {
					err = errors.New("the default vault isn't a vault")
				}
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&pick, "pick", false, "choose the default from the list of vaults")
	cmd.Flags().BoolVar(&unset, "unset", false, "remove the default")
	cmd.MarkFlagsMutuallyExclusive("pick", "unset")
	return cmd
}

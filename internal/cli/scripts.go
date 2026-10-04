package cli

import "github.com/spf13/cobra"

// Subcommands still implemented by bash scripts (libexec/claude-tickets);
// each moves to Go in a later step of the port.

func syncCmd() *cobra.Command {
	return scriptCmd("sync [--source <name>] [--full]", "Pull your open tickets from each source into the vault", "ticket-sync")
}

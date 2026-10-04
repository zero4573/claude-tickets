package cli

import "github.com/spf13/cobra"

// Subcommands still implemented by bash scripts (libexec/claude-tickets);
// each moves to Go in a later step of the port.

func syncCmd() *cobra.Command {
	return scriptCmd("sync [--source <name>] [--full]", "Pull your open tickets from each source into the vault", "ticket-sync")
}

func wsCmd() *cobra.Command {
	return scriptCmd("ws repos|clone|add|ls|diff|sign|rm|fetch|gc ...", "Git worktrees of ticket workspaces", "ticket-ws")
}

func graphCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "graph", Short: "The code graph: graphify image, MCP server, main-clone graphs"}
	cmd.AddCommand(
		scriptCmd("build [--force]", "Build the graphify image and unpack its filesystem", "ticket-graph", "build"),
		scriptCmd("status", "What's built, for which hash", "ticket-graph", "status"),
		scriptCmd("mcp <workspace>", "The MCP server of a workspace's merged graph (stdio)", "ticket-graph", "mcp"),
		scriptCmd("index [<repo>...]", "Build or refresh the main clones' code graphs", "graphify-index"),
	)
	return cmd
}

func kbCmd() *cobra.Command {
	cmd := scriptCmd(`kb [--continue] [--print] [--no-fetch] ["<question>"]`, "Ask about the system the vault describes, without a ticket", "kb")
	// `ct kb repo …` is kb-repo (exploration clones, inside a kb session)
	run := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if len(args) > 0 && args[0] == "repo" {
			return runScript("kb-repo", args[1:])
		}
		return run(c, args)
	}
	return cmd
}

func claudeCmd() *cobra.Command {
	return scriptCmd("claude [claude args...]", "Claude in the current repo, with the vault as its knowledge base", "claude-vault")
}

func layoutCmd() *cobra.Command {
	return scriptCmd("layout [--apply] [--root <dir>]", "Move repos into <projectsRoot>/<provider>/<owner>/<repo>", "repo-layout")
}

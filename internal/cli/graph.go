package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/followup"
	"github.com/zero4573/claude-tickets/internal/graph"
	"github.com/zero4573/claude-tickets/internal/repo"
	"github.com/zero4573/claude-tickets/internal/vault"
)

func graphCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "The code graph: graphify image, MCP server, main-clone graphs",
		Long: `The code graph of ticket and kb sessions: one graph merging the workspace's
checkouts with every main clone's graph, served as an MCP server (named
graphify) over stdin/stdout. graphify runs in an image built here, with
podman or docker (CLAUDE_TICKETS_CONTAINER, else detected).`,
	}
	var force bool
	build := &cobra.Command{
		Use:   "build [--force]",
		Short: "Build the graphify image and unpack its filesystem",
		Long: `Builds localhost/claude-tickets-graphify:<hash> (pinned Python image + the
hashed graphify lock), and unpacks its filesystem into
` + config.TildePath(filepath.Join(config.CacheDir(), "graphify")) + `/rootfs-<hash> (Linux). Run it on the host; ct start,
ct kb and ct graph index run it when needed. Only rebuilt when the image's
sources change (or with --force). Prints the image's tag.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			tag, err := graph.Build(force)
			if err != nil {
				return err
			}
			fmt.Println(tag)
			return nil
		},
	}
	build.Flags().BoolVar(&force, "force", false, "rebuild and unpack even if up to date")
	status := &cobra.Command{
		Use:   "status",
		Short: "What's built, for which hash",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return graph.Status(os.Stdout) },
	}
	mcp := &cobra.Command{
		Use:   "mcp <workspace>",
		Short: "The MCP server of a workspace's merged graph (stdio)",
		Long: `The MCP server for <workspace> (what sessions run). It uses the image when
the container runtime has it, else the unpacked filesystem with
podman run --rootfs, which works where the runtime starts with no images
(e.g. podman inside a container).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			argv, err := graph.MCPCommand(args[0], ctx.ProjectsRoot)
			if err != nil {
				return err
			}
			path, err := exec.LookPath(argv[0])
			if err != nil {
				return err
			}
			return passExit(execReplaceOrRun(path, argv))
		},
	}
	index := &cobra.Command{
		Use:   "index [<repo>...]",
		Short: "Build or refresh the main clones' code graphs",
		Long: `Builds or refreshes the code graph (graphify-out/graph.json, code-only AST
pass, no LLM, incremental) of each main clone under
<projectsRoot>/<provider>/<owner>/<repo>: all of them, or the given ones (a
<repo> is a slug or <provider>/<owner>/<repo>). Ticket sessions merge these
graphs for every repo the ticket has no checkout of. Runs graphify in the
claude-tickets graphify image (ct graph build; podman or docker). Output is
also logged to ` + logPath(graph.IndexLog) + `; repos that fail become
follow-up tasks in the vault's inbox.`,
		ValidArgsFunction: completeRepos,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := vault.Optional()
			var repos []string
			for _, s := range args {
				c, ok := repo.Resolve(ctx.ProjectsRoot, s)
				if !ok {
					return fmt.Errorf("no main clone '%s' (see ct ws repos)", s)
				}
				repos = append(repos, c)
			}
			if len(repos) == 0 {
				repos = repo.MainClones(ctx.ProjectsRoot)
			}
			if len(repos) == 0 {
				return fmt.Errorf("no main clones under %s (expected <provider>/<owner>/<repo>; see ct layout)", ctx.ProjectsRoot)
			}
			if err := indexGraphs(ctx, repos); err != nil {
				return exitError(1)
			}
			return nil
		},
	}
	cmd.AddCommand(build, status, mcp, index)
	return cmd
}

// indexGraphs runs graph.Index; repos whose graph failed become tasks in
// a follow-up note in the vault's inbox (when there's a vault).
func indexGraphs(ctx vault.Context, repos []string) error {
	failed, err := graph.Index(ctx.ProjectsRoot, repos)
	if err == nil {
		return nil
	}
	if ctx.Vault != "" && errors.Is(err, graph.ErrIndexFailed) {
		log := config.TildePath(config.StateLog(graph.IndexLog))
		var tasks []string
		for _, r := range failed {
			tasks = append(tasks, fmt.Sprintf("ct graph index: building the code graph of %s failed; check %s, then rerun ct graph index %s", r, log, r))
		}
		if len(tasks) == 0 {
			tasks = []string{fmt.Sprintf("ct graph index failed before indexing; check %s", log)}
		}
		if n, werr := followup.Write(ctx.Vault, "ct-graph-index", "ct graph index", tasks); werr == nil && n != "" {
			rel, _ := filepath.Rel(ctx.Vault, n)
			fmt.Printf("ct graph index: follow-ups in %s\n", rel)
		}
	}
	if !errors.Is(err, graph.ErrIndexFailed) {
		fmt.Fprintln(os.Stderr, "ct: "+err.Error())
	}
	return err
}

// passExit turns a command's exit status into ct's.
func passExit(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return exitError(ee.ExitCode())
	}
	return err
}

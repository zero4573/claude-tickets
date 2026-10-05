package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/graph"
	"github.com/zero4573/claude-tickets/internal/launcher"
	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/repo"
	"github.com/zero4573/claude-tickets/internal/session"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

type startOpts struct {
	force, noFetch, feedback, noAttach, list, all bool
}

func startCmd() *cobra.Command {
	var o startOpts
	cmd := &cobra.Command{
		Use:   "start [--force] [--no-fetch] [--feedback] [--no-attach] <ID>... | --list [--all]",
		Short: "Start (or re-open) one session per named ticket",
		Long: `Starts (or re-opens) one Claude session for each ticket named, from the
current vault (ct vault default), each in its own window of the vault's tmux
session (tickets-<vault>), running /tickets:work-ticket <ID> in the workspace
<workRoot>/<ID> (by default ~/Projects/work-<vault>/<ID>; see ct vault).
Each must be a ticket of the vault: tickets/<ID>/<ID>.md, synced by ct sync or a manual
ticket from ct new. If any isn't, none start.

A workspace that already ran a session continues its last conversation.
The workspace is marked as trusted in Claude Code, so the session starts
without asking. With one ID, run from a terminal, it then switches to that
window (--no-attach doesn't). Attach later with ct attach <ID>, see all of
them with ct status.

--feedback runs /tickets:pr-feedback <ID> instead: apply the review feedback
on your open PRs for the ticket (ct feedback is a shorthand). A ticket whose
window is already open gets /tickets:pr-feedback typed into it.

--list prints the vault's open tickets, one per line: <ID> <status>
<summary>, tab-separated (--all: done and closed tickets too).`,
		ValidArgsFunction: completeIDs(openTickets),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			if o.list {
				printTickets(note.List(ctx.Vault, o.all))
				return nil
			}
			if len(args) == 0 {
				return errors.New("name at least one ticket (ct start --list shows them)")
			}
			return start(ctx, args, o)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&o.force, "force", false, "start tickets marked ignore: true too, or one covered by an open lead ticket on its own")
	f.BoolVar(&o.noFetch, "no-fetch", false, "skip fetching the main clones first")
	f.BoolVar(&o.feedback, "feedback", false, "run /tickets:pr-feedback instead of /tickets:work-ticket")
	f.BoolVar(&o.noAttach, "no-attach", false, "with one ID, don't switch to its window")
	f.BoolVar(&o.list, "list", false, "print the vault's open tickets")
	f.BoolVar(&o.all, "all", false, "with --list: done and closed tickets too")
	return cmd
}

func feedbackCmd() *cobra.Command {
	var o startOpts
	cmd := &cobra.Command{
		Use:               "feedback [--force] [--no-fetch] [--no-attach] <ID>...",
		Short:             "ct start --feedback: apply the review feedback on your open PRs",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeIDs(openTickets),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			o.feedback = true
			return start(ctx, args, o)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&o.force, "force", false, "start tickets marked ignore: true too, or one covered by an open lead ticket on its own")
	f.BoolVar(&o.noFetch, "no-fetch", false, "skip fetching the main clones first")
	f.BoolVar(&o.noAttach, "no-attach", false, "with one ID, don't switch to its window")
	return cmd
}

// linkTarget strips a frontmatter link's brackets and quotes: [[PROJ-1]] -> PROJ-1.
func linkTarget(s string) string { return strings.NewReplacer("[", "", "]", "", `"`, "").Replace(s) }

func start(ctx vault.Context, args []string, o startOpts) error {
	var keys []string
	seen := map[string]bool{}
	for _, k := range args {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	// Every ID must be one of the vault's tickets, or nothing starts
	var missing []string
	for _, k := range keys {
		if !note.ValidKey(k) {
			return fmt.Errorf("not a ticket ID: '%s'", k)
		}
		if _, err := os.Stat(note.TicketPath(ctx.Vault, k)); err != nil {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("not a ticket of the %s vault: %s (no tickets/<ID>/<ID>.md; ct sync, or ct new for a manual ticket; ct start --list shows them); nothing started",
			filepath.Base(ctx.Vault), strings.Join(missing, " "))
	}
	skill := "work-ticket"
	if o.feedback {
		skill = "pr-feedback"
	}

	var launch []string
	for _, k := range keys {
		file := note.TicketPath(ctx.Vault, k)
		fm := note.Frontmatter(file)
		if !o.force && note.Ignored(file) {
			warnf("%s: marked ignore: true (%s), skipping (use --force)", k, fm["ignore-reason"])
			continue
		}
		// A ticket covered by a lead (e.g. an epic's child) is worked in the
		// lead's workspace, on its branch
		if lead := linkTarget(fm["covered-by"]); lead != "" && !o.force {
			st := note.Get(note.TicketPath(ctx.Vault, lead), "status")
			if st != "done" && st != "closed" {
				if o.feedback {
					warnf("%s: covered by %s, so its PRs are handled there: ct feedback %s (or --force)", k, lead, lead)
				} else {
					warnf("%s: covered by %s, so it's worked there: ct start %s (or --force to start it on its own)", k, lead, lead)
				}
				continue
			}
		}
		if fm["blocked"] == "true" {
			warnf("%s: blocked by %s; the kickoff will ask how to go ahead", k, linkTarget(fm["blocked-by"]))
		}
		launch = append(launch, k)
	}
	if len(launch) == 0 {
		return exitError(1)
	}

	if !o.noFetch {
		if err := wsFetch(ctx, nil); err != nil {
			return err
		}
	}
	// What a session works in, given to claude as --add-dir: the vault, the
	// main clones (read-only to sessions: session.WriteSettings), each main
	// clone's .git (writable, so ct ws add can create worktrees and branches)
	// and the graph image cache (read-only)
	dirs := []string{ctx.Vault, ctx.ProjectsRoot}
	for _, c := range repo.MainClones(ctx.ProjectsRoot) {
		dirs = append(dirs, filepath.Join(ctx.ProjectsRoot, c, ".git"))
	}
	dirs = append(dirs, filepath.Join(config.CacheDir(), "graphify"))
	// The merged code graph of each workspace (ct graph mcp) needs its image
	graphOK := true
	if _, err := graph.Build(false); err != nil {
		warnf("%v", err)
		warnf("the graphify image couldn't be built; sessions start without the merged code graph")
		graphOK = false
	}

	for _, k := range launch {
		if err := startOne(ctx, k, skill, dirs, graphOK); err != nil {
			return err
		}
	}

	if !o.noAttach && len(keys) == 1 && term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) &&
		launcher.HasWindow(launcher.Get(), ctx.TmuxSession, keys[0]) {
		return launcher.Get().Attach(ctx.TmuxSession, keys[0])
	}
	return nil
}

func startOne(ctx vault.Context, key, skill string, dirs []string, graphOK bool) error {
	dir := filepath.Join(ctx.WorkRoot, key)
	_, err := os.Stat(filepath.Join(dir, ".agent-state"))
	resume := err == nil
	if err := workspace.Ensure(dir, key, ctx.Vault); err != nil {
		return err
	}
	if err := session.WriteSettings(dir, ctx.ProjectsRoot); err != nil {
		return err
	}
	if err := session.Trust(dir); err != nil {
		warnf("couldn't mark %s as trusted in %s: %v", dir, session.ClaudeConfig(), err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(ticketClaudeMD(ctx, key, dir)), 0o644); err != nil {
		return err
	}

	prompt := "/tickets:" + skill + " " + key
	argv, err := session.Command(dirs...)
	if err != nil {
		return err
	}
	if graphOK {
		mcp, err := session.WriteGraphMCP(dir, ctx.Vault)
		if err != nil {
			return err
		}
		argv = append(argv, "--mcp-config", mcp)
	}
	argv = append(argv, "--permission-mode", "auto")
	if resume {
		argv = append(argv, "--continue")
	}
	argv = append(argv, prompt)

	l := launcher.Get()
	if launcher.HasWindow(l, ctx.TmuxSession, key) {
		if skill == "pr-feedback" {
			if err := l.SendKeys(ctx.TmuxSession, key, prompt); err != nil {
				return err
			}
			fmt.Printf("ct start: %s already open, sent %s to its window -- ct attach %s\n", key, prompt, key)
		} else {
			warnf("%s: already has a window in %s session '%s', not starting another", key, l.Name(), ctx.TmuxSession)
		}
		return nil
	}
	// The vault goes with the session (not with a backend server started now:
	// shells opened there later would act on this vault, whatever the default)
	if err := l.Open(launcher.Window{Group: ctx.TmuxSession, Name: key, Dir: dir, Argv: argv,
		Env: map[string]string{"CLAUDE_TICKETS_VAULT": ctx.Vault}, Unset: []string{"CLAUDE_TICKETS_VAULT"}}); err != nil {
		return fmt.Errorf("couldn't open a window for %s: %w", key, err)
	}
	cont := ""
	if resume {
		cont = " (continuing its last session)"
	}
	fmt.Printf("ct start: %s started in %s%s -- ct attach %s\n", key, ctx.TmuxSession, cont, key)
	return nil
}

func ticketClaudeMD(ctx vault.Context, key, dir string) string {
	v, p := ctx.Vault, ctx.ProjectsRoot
	ticket := note.TicketPath(v, key)
	return fmt.Sprintf("# Ticket workspace: %[1]s\n\n"+
		"Written by ct start; regenerated on every start, so don't edit it.\n\n"+
		"- Ticket: `%[1]s` (source: `%[2]s`)\n"+
		"- Vault: `%[3]s` (read its `AGENTS.md` for note rules)\n"+
		"- Ticket note: `%[4]s`\n"+
		"- Ticket folder (the only place in the vault this session writes to before\n"+
		"  `/tickets:save`): `%[3]s/tickets/%[1]s/`. A lead ticket (`ticket-type: epic`)\n"+
		"  may also write the work sections and `status` of the tickets in its\n"+
		"  `covers`.\n"+
		"- Repos in this workspace: `%[5]s/workspace.json` (each repo's path, base\n"+
		"  and branch), kept current by `ct ws add`. Checkouts are at\n"+
		"  `%[5]s/<slug>`, normally on `feature/%[1]s[-<description>]`.\n"+
		"- Main clones: `%[6]s/<provider>/<owner>/<repo>`. Never edit\n"+
		"  them: their `.git` is only writable so `ct ws add` can create\n"+
		"  worktrees. Each\n"+
		"  repo's slug is `<provider>-<owner>-<repo>` (`ct ws repos` lists\n"+
		"  them); the vault and the code graph name repos by slug.\n"+
		"- Vault tools: `ct vault lock` and `ct vault links` (used by `/tickets:save`).\n"+
		"  `ct new` files a manual ticket.\n"+
		"- Code graph: the `graphify` MCP server merges this workspace's worktrees\n"+
		"  with every other repo's main clone. Query it before reading files.\n\n"+
		"- Git: commit your work on the ticket branch in coherent steps (subject\n"+
		"  starting with `%[1]s`, unless the repo's own convention says otherwise).\n"+
		"  Commits are unsigned here; the user signs them on the host with\n"+
		"  `ct ws sign %[1]s`. Never push, rewrite pushed commits, reset\n"+
		"  --hard, or remove worktrees.\n\n"+
		"Run `/tickets:work-ticket %[1]s` to (re)start the workflow, and\n"+
		"`/tickets:save` once the user has reviewed the changes. The user signs and pushes.\n",
		key, note.Get(ticket, "source"), v, ticket, dir, p)
}

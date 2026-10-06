package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	// name is the command as typed (ct start, ct feedback), for messages
	name string
}

// isTerminal: stdin and stdout are a terminal (a session can run here, a
// window can be attached to).
var isTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) }

// AssumeTerminal makes ct treat stdin and stdout as a terminal. For tests
// only: the ct-tty command of the CLI tests.
func AssumeTerminal() { isTerminal = func() bool { return true } }

const forceHelp = "start tickets marked ignore: true too, one covered by an open lead ticket on its own, or (without a multiplexer) one whose session only looks running from its .agent-state"
const noAttachHelp = "with one ID, don't switch to its window (needs a terminal multiplexer)"

// launcherHelp is the part of ct start's and ct feedback's help about
// running without a terminal multiplexer.
const launcherHelp = `Without a terminal multiplexer (CLAUDE_TICKETS_LAUNCHER, or config.json's
launcher: tmux or none; by default tmux when it's installed, else none), the
session runs in this terminal, in the foreground: one ticket at a time,
from a terminal, and --no-attach is refused. A ticket whose session is
running (its process is alive on this host) isn't started again; one whose
session only looks running (its .agent-state is less than a day old and not
exited, e.g. after its terminal was closed) needs --force.`

func startCmd() *cobra.Command {
	o := startOpts{name: "ct start"}
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

` + launcherHelp + `

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
	f.BoolVar(&o.force, "force", false, forceHelp)
	f.BoolVar(&o.noFetch, "no-fetch", false, "skip fetching the main clones first")
	f.BoolVar(&o.feedback, "feedback", false, "run /tickets:pr-feedback instead of /tickets:work-ticket")
	f.BoolVar(&o.noAttach, "no-attach", false, noAttachHelp)
	f.BoolVar(&o.list, "list", false, "print the vault's open tickets")
	f.BoolVar(&o.all, "all", false, "with --list: done and closed tickets too")
	return cmd
}

func feedbackCmd() *cobra.Command {
	o := startOpts{name: "ct feedback"}
	cmd := &cobra.Command{
		Use:   "feedback [--force] [--no-fetch] [--no-attach] <ID>...",
		Short: "ct start --feedback: apply the review feedback on your open PRs",
		Long: `Starts a session per ticket named running /tickets:pr-feedback <ID>: it
applies the review feedback on your open PRs for the ticket, as ct start
--feedback does. A ticket whose window is already open gets
/tickets:pr-feedback typed into it.

` + launcherHelp + ` There, a running session gets no
keys typed into it: type /tickets:pr-feedback in its terminal yourself.`,
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
	f.BoolVar(&o.force, "force", false, forceHelp)
	f.BoolVar(&o.noFetch, "no-fetch", false, "skip fetching the main clones first")
	f.BoolVar(&o.noAttach, "no-attach", false, noAttachHelp)
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
	l, err := launcher.Resolve()
	if err != nil {
		return err
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
	// Without background windows, the one session runs here, in the foreground
	foreground := !l.Caps().Background
	if foreground {
		switch {
		case len(keys) > 1:
			return fmt.Errorf("without a terminal multiplexer, %s takes one ticket (got %d); set %s=tmux to run several", o.name, len(keys), launcher.EnvVar)
		case o.noAttach:
			return errors.New("--no-attach needs a terminal multiplexer: without one, the session runs in this terminal")
		case !isTerminal():
			return fmt.Errorf("without a terminal multiplexer, the session runs in this terminal, and this isn't one (stdin/stdout); run it from a terminal, or set %s=tmux", launcher.EnvVar)
		}
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
	if foreground {
		if err := refuseRunning(ctx, l, launch[0], o); err != nil {
			return err
		}
	}

	if !o.noFetch {
		if err := wsFetch(ctx, nil); err != nil {
			return err
		}
	}
	dirs := sessionDirs(ctx)
	// The merged code graph of each workspace (ct graph mcp) needs its image
	graphOK := true
	if _, err := graph.Build(false); err != nil {
		warnf("%v", err)
		warnf("the graphify image couldn't be built; sessions start without the merged code graph")
		graphOK = false
	}

	for _, k := range launch {
		w, resume, err := prepareSession(ctx, k, skill, dirs, graphOK)
		if err != nil {
			return err
		}
		if foreground {
			return runForeground(l, w, resume, o)
		}
		if err := openWindow(ctx, l, w, skill, resume); err != nil {
			return err
		}
	}

	if l.Caps().Attach && !o.noAttach && len(keys) == 1 && isTerminal() &&
		launcher.HasWindow(l, ctx.TmuxSession, keys[0]) {
		return l.Attach(ctx.TmuxSession, keys[0])
	}
	return nil
}

// sessionDirs is what a ticket session works in, given to claude as
// --add-dir: the vault, the main clones (read-only to sessions:
// session.WriteSettings), each main clone's .git (writable, so ct ws add can
// create worktrees and branches) and the graph image cache (read-only).
func sessionDirs(ctx vault.Context) []string {
	dirs := []string{ctx.Vault, ctx.ProjectsRoot}
	for _, c := range repo.MainClones(ctx.ProjectsRoot) {
		dirs = append(dirs, filepath.Join(ctx.ProjectsRoot, c, ".git"))
	}
	return append(dirs, filepath.Join(config.CacheDir(), "graphify"))
}

// refuseRunning keeps a second foreground session out of a workspace whose
// session runs: always when its process is alive here, unless --force when
// only its .agent-state says so (its terminal may have been closed).
func refuseRunning(ctx vault.Context, l launcher.Launcher, key string, o startOpts) error {
	st := workspace.Session(filepath.Join(ctx.WorkRoot, key), l, ctx.TmuxSession)
	switch {
	case !st.Running:
		return nil
	case st.By == workspace.ByAgentState:
		if o.force {
			return nil
		}
		ts := st.State.TS
		if ts == "" {
			ts = "-"
		}
		force := o.name + " --force " + key
		if o.feedback && o.name == "ct start" {
			force = "ct start --feedback --force " + key
		}
		if o.feedback {
			return fmt.Errorf("%s: its session looks running (.agent-state: %s at %s); type /tickets:pr-feedback in that session's terminal, or if it isn't running, %s",
				key, st.State.State, ts, force)
		}
		return fmt.Errorf("%s: its session looks running (.agent-state: %s at %s); it's in the terminal where it was started. If it isn't (terminal closed), %s",
			key, st.State.State, ts, force)
	case st.By == workspace.ByPID:
		if o.feedback {
			return fmt.Errorf("%s: its session is running (pid %d) in another terminal; type /tickets:pr-feedback there", key, st.PID)
		}
		started := ""
		if t, err := time.Parse(time.RFC3339, st.Started); err == nil {
			started = ", started " + t.Format("15:04")
		}
		return fmt.Errorf("%s: its session is running (pid %d%s) in another terminal; continue there, or end it first", key, st.PID, started)
	default:
		return fmt.Errorf("%s: its session is running in another window; continue there, or end it first", key)
	}
}

// prepareTicketSession readies a ticket's workspace for a session (its
// workspace.json, settings, trust, CLAUDE.md) and returns it with the
// claude command line to run there (no prompt yet).
func prepareTicketSession(ctx vault.Context, key string, dirs []string) (string, []string, error) {
	dir := filepath.Join(ctx.WorkRoot, key)
	if err := workspace.Ensure(dir, key, ctx.Vault); err != nil {
		return "", nil, err
	}
	if err := session.WriteSettings(dir, ctx.ProjectsRoot); err != nil {
		return "", nil, err
	}
	if err := session.Trust(dir); err != nil {
		warnf("couldn't mark %s as trusted in %s: %v", dir, session.ClaudeConfig(), err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(ticketClaudeMD(ctx, key, dir)), 0o644); err != nil {
		return "", nil, err
	}
	argv, err := session.Command(dirs...)
	return dir, argv, err
}

// prepareSession readies a ticket's workspace (prepareTicketSession, plus the
// graph MCP config) and builds its session; resume when the workspace already
// ran one (it continues it).
func prepareSession(ctx vault.Context, key, skill string, dirs []string, graphOK bool) (launcher.Window, bool, error) {
	_, err := os.Stat(filepath.Join(ctx.WorkRoot, key, ".agent-state"))
	resume := err == nil
	dir, argv, err := prepareTicketSession(ctx, key, dirs)
	if err != nil {
		return launcher.Window{}, false, err
	}
	if graphOK {
		mcp, err := session.WriteGraphMCP(dir, ctx.Vault)
		if err != nil {
			return launcher.Window{}, false, err
		}
		argv = append(argv, "--mcp-config", mcp)
	}
	argv = append(argv, "--permission-mode", "auto")
	if resume {
		argv = append(argv, "--continue")
	}
	argv = append(argv, sessionPrompt(skill, key))
	// The vault goes with the session (not with a backend server started now:
	// shells opened there later would act on this vault, whatever the default)
	return launcher.Window{Group: ctx.TmuxSession, Name: key, Dir: dir, Argv: argv,
		Env: map[string]string{"CLAUDE_TICKETS_VAULT": ctx.Vault}, Unset: []string{"CLAUDE_TICKETS_VAULT"}}, resume, nil
}

func sessionPrompt(skill, key string) string { return "/tickets:" + skill + " " + key }

// openWindow starts a session in a background window, or (feedback) types
// the prompt into its window when it's open already.
func openWindow(ctx vault.Context, l launcher.Launcher, w launcher.Window, skill string, resume bool) error {
	key := w.Name
	if launcher.HasWindow(l, ctx.TmuxSession, key) {
		if skill == "pr-feedback" {
			prompt := sessionPrompt(skill, key)
			if err := l.SendKeys(ctx.TmuxSession, key, prompt); err != nil {
				return err
			}
			fmt.Printf("ct start: %s already open, sent %s to its window -- ct attach %s\n", key, prompt, key)
		} else {
			warnf("%s: already has a window in %s session '%s', not starting another", key, l.Name(), ctx.TmuxSession)
		}
		return nil
	}
	// A record left by a foreground session that has ended would otherwise
	// make this window's session look dead
	if err := workspace.PruneSessions(w.Dir); err != nil {
		warnf("couldn't update %s: %v", workspace.SessionsFile(w.Dir), err)
	}
	if err := l.Open(w); err != nil {
		return fmt.Errorf("couldn't open a window for %s: %w", key, err)
	}
	cont := ""
	if resume {
		cont = " (continuing its last session)"
	}
	fmt.Printf("ct start: %s started in %s%s -- ct attach %s\n", key, ctx.TmuxSession, cont, key)
	return nil
}

// runForeground runs the session in this terminal: ct becomes claude
// (Windows: waits for it), and the session's process is recorded so other
// commands see it running.
func runForeground(l launcher.Resolved, w launcher.Window, resume bool, o startOpts) error {
	cont := ""
	if resume {
		cont = " (continuing its last session)"
	}
	fmt.Printf("ct start: %s runs in this terminal%s; no window to come back to, ct start %s later continues it\n", w.Name, cont, w.Name)
	if l.Source == launcher.FromDetected {
		fmt.Fprintln(os.Stderr, "ct start: tmux not found, so the session runs here; install tmux for background windows and several sessions at once")
	}
	command := "start"
	if o.feedback {
		command = "feedback"
	}
	if err := workspace.RecordSession(w.Dir, command); err != nil {
		warnf("couldn't record the session in %s: %v", workspace.SessionsFile(w.Dir), err)
	}
	return passExit(launcher.Run(w))
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

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/graph"
	"github.com/zero4573/claude-tickets/internal/launcher"
	"github.com/zero4573/claude-tickets/internal/modelcache"
	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/session"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

type startOpts struct {
	force, noFetch, feedback, cont, list, all bool
	// dev is --dev (a checkout slug, or none)
	dev string
	// name is the command as typed (ct start, ct feedback), for messages
	name string
}

// isTerminal: stdin and stdout are a terminal (a session can run here).
var isTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) }

// AssumeTerminal makes ct treat stdin and stdout as a terminal. For tests
// only: the ct-tty command of the CLI tests.
func AssumeTerminal() {
	isTerminal = func() bool { return true }
}

const forceHelp = "start tickets marked ignore: true too, one covered by an open lead ticket on its own, or one whose session only looks running from its .agent-state"
const continueHelp = "continue the workspace's last conversation instead of starting a new one"

// foregroundHelp is the part of ct start's and ct feedback's help about
// where the session runs.
const foregroundHelp = `The session runs in this terminal, in the foreground (ct becomes claude):
one ticket per run, from a terminal. A ticket whose session is running (its
process is alive on this host) isn't started again; one whose session only
looks running (its .agent-state is less than a day old and not exited, e.g.
after its terminal was closed) needs --force.`

func startCmd() *cobra.Command {
	o := startOpts{name: "ct start"}
	cmd := &cobra.Command{
		Use:   "start [--force] [--no-fetch] [--feedback] [--continue] [--dev <slug>|none] <ID> | --list [--all]",
		Short: "Start (or re-open) a ticket's session in this terminal",
		Long: `Starts (or re-opens) a Claude session for the ticket named, from the
current vault (ct vault default), in this terminal, running
/tickets:work-ticket <ID> in the workspace <workRoot>/<ID> (by default
~/Projects/work-<vault>/<ID>; see ct vault). It must be a ticket of the
vault: tickets/<source>/<ID>/<ID>.md, synced by ct sync or a manual ticket
from ct new.

Each session is a new conversation, even in a workspace that already ran
one: /tickets:work-ticket resumes from the ticket's board.md, so a fresh
conversation every wave or two keeps the context small. --continue (-c)
continues the workspace's last conversation instead. The workspace is marked as trusted in Claude Code, so the session starts
without asking. ct status shows every ticket's session.

` + foregroundHelp + `

--feedback runs /tickets:pr-feedback <ID> instead: apply the review feedback
on your open PRs for the ticket (ct feedback is a shorthand). For a ticket
whose session is running, type /tickets:pr-feedback in its terminal.

--dev <slug> makes the workspace a dev workspace, for a ticket that changes
claude-tickets itself: at each start ct builds ct from the workspace's
checkout <slug> (go build, into <workspace>/.cache/dev) and copies its
plugin there, and its sessions run that ct (first on PATH, and for the code
graph) and that plugin instead of the installed ones. ct never runs the
build itself; a running session keeps the build it started with, and ct clean
saves on the installed ones. It's kept in workspace.json for later starts;
--dev none clears it.

--list prints the vault's open tickets, one per line: <ID> <status>
<summary>, tab-separated (--all: done and closed tickets too).`,
		ValidArgsFunction: completeOneID(openTickets),
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
	f.BoolVarP(&o.cont, "continue", "c", false, continueHelp)
	f.BoolVar(&o.list, "list", false, "print the vault's open tickets")
	f.BoolVar(&o.all, "all", false, "with --list: done and closed tickets too")
	f.StringVar(&o.dev, "dev", "", devHelp)
	return cmd
}

func feedbackCmd() *cobra.Command {
	o := startOpts{name: "ct feedback"}
	cmd := &cobra.Command{
		Use:   "feedback [--force] [--no-fetch] [--continue] <ID>",
		Short: "ct start --feedback: apply the review feedback on your open PRs",
		Long: `Starts a session for the ticket named running /tickets:pr-feedback <ID>:
it applies the review feedback on your open PRs for the ticket, as ct start
--feedback does. Each session is a new conversation; --continue (-c)
continues the workspace's last conversation instead.

` + foregroundHelp + ` A running session gets no keys typed into it: type
/tickets:pr-feedback in its terminal yourself.`,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeOneID(openTickets),
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
	f.BoolVarP(&o.cont, "continue", "c", false, continueHelp)
	return cmd
}

// completeOneID completes a single ticket ID: ct start and ct feedback take one.
func completeOneID(list func(vault.Context) []note.Ticket) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	ids := completeIDs(list)
	return func(cmd *cobra.Command, args []string, s string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return ids(cmd, args, s)
	}
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
		return fmt.Errorf("not a ticket of the %s vault: %s (no tickets/<source>/<ID>/<ID>.md; ct sync, or ct new for a manual ticket; ct start --list shows them); nothing started",
			filepath.Base(ctx.Vault), strings.Join(missing, " "))
	}
	skill := "work-ticket"
	if o.feedback {
		skill = "pr-feedback"
	}
	// The one session runs here, in the foreground
	switch {
	case len(keys) > 1:
		return fmt.Errorf("%s takes one ticket (got %d): the session runs in this terminal; start the others from their own terminals", o.name, len(keys))
	case !isTerminal():
		return errors.New("the session runs in this terminal, and this isn't one (stdin/stdout); run it from a terminal")
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
	if err := refuseRunning(ctx, launch[0], o); err != nil {
		return err
	}
	if o.dev != "" {
		if err := setDev(ctx, launch, o.dev); err != nil {
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

	k := launch[0]
	w, resume, err := prepareSession(ctx, k, skill, dirs, graphOK, o.cont, devRebuild)
	if err != nil {
		return err
	}
	return runForeground(k, w, resume, o)
}

// sessionDirs is what a ticket session works in, given to claude as
// --add-dir: the vault, the repo directory (writable: ct ws clone and ct ws
// add's worktrees; one --add-dir, not one per repo, which would grow every
// session's context), the graph image cache and the embedding model cache
// (both read-only; each is added only when it exists).
func sessionDirs(ctx vault.Context) []string {
	return []string{ctx.Vault, ctx.ProjectsRoot, filepath.Join(config.CacheDir(), "graphify"), modelcache.Dir()}
}

// refuseRunning keeps a second session out of a workspace whose session
// runs: always when its process is alive here, unless --force when only its
// .agent-state says so (its terminal may have been closed).
func refuseRunning(ctx vault.Context, key string, o startOpts) error {
	st := workspace.Session(filepath.Join(ctx.WorkRoot, key))
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
	default:
		return runningPIDError(key, st.PID, st.Started, o)
	}
}

// runningPIDError refuses a ticket whose session's process is alive here
// (--force doesn't override it: that session isn't stale).
func runningPIDError(key string, pid int, startedAt string, o startOpts) error {
	if o.feedback {
		return fmt.Errorf("%s: its session is running (pid %d) in another terminal; type /tickets:pr-feedback there", key, pid)
	}
	started := ""
	if t, err := time.Parse(time.RFC3339, startedAt); err == nil {
		started = ", started " + t.Format("15:04")
	}
	return fmt.Errorf("%s: its session is running (pid %d%s) in another terminal; continue there, or end it first", key, pid, started)
}

// prepareTicketSession readies a ticket's workspace for a session (its
// workspace.json, settings, trust, CLAUDE.md) and returns it with the
// claude command line to run there (no prompt yet) and, in a dev workspace
// (ct start --dev; devMode says what to do with its build), the build.
func prepareTicketSession(ctx vault.Context, key string, dirs []string, mode devMode) (string, []string, *devBuild, error) {
	dir := filepath.Join(ctx.WorkRoot, key)
	if err := workspace.Ensure(dir, key, ctx.Vault); err != nil {
		return "", nil, nil, err
	}
	dev, err := prepareDev(dir, mode)
	if err != nil {
		return "", nil, nil, err
	}
	if err := session.WriteSettings(dir); err != nil {
		return "", nil, nil, err
	}
	writeToolVersions(ctx, dir)
	if err := session.Trust(dir); err != nil {
		warnf("couldn't mark %s as trusted in %s: %v", dir, session.ClaudeConfig(), err)
	}
	if err := writeTicketClaudeMD(ctx, key, dir); err != nil {
		return "", nil, nil, err
	}
	if dev != nil {
		return dir, session.CommandPlugin(dev.plugin, dirs...), dev, nil
	}
	argv, err := session.Command(dirs...)
	return dir, argv, nil, err
}

// prepareSession readies a ticket's workspace (prepareTicketSession, plus the
// graph MCP config) and builds its session: a new conversation, or with cont
// (--continue) the workspace's last one (resume) when it already ran one.
func prepareSession(ctx vault.Context, key, skill string, dirs []string, graphOK, cont bool, mode devMode) (launcher.Session, bool, error) {
	_, err := os.Stat(filepath.Join(ctx.WorkRoot, key, ".agent-state"))
	resume := err == nil && cont
	if cont && !resume {
		warnf("%s: no earlier session in its workspace to continue, starting a new one", key)
	}
	dir, argv, dev, err := prepareTicketSession(ctx, key, dirs, mode)
	if err != nil {
		return launcher.Session{}, false, err
	}
	env := map[string]string{"CLAUDE_TICKETS_VAULT": ctx.Vault}
	devCt, pathFirst := "", ""
	if dev != nil {
		devCt, pathFirst = dev.ct, dev.bin
		// For a wrapper (sandbox, container) that sets its own PATH: it
		// prepends this to have the built ct first
		env["CLAUDE_TICKETS_DEV_BIN"] = dev.bin
	}
	if graphOK {
		// Merged now, however long it takes: the session's server then only
		// loads it. A failed merge stops here, rather than a session without
		// the graph
		fmt.Fprintf(os.Stderr, "ct start: %s: bringing the merged code graph up to date\n", key)
		if err := graph.Merge(dir, ctx.ProjectsRoot); err != nil {
			return launcher.Session{}, false, fmt.Errorf("%w (ct graph mcp %s shows the server's own run)", err, dir)
		}
		mcp, err := session.WriteGraphMCPWith(dir, ctx.Vault, devCt)
		if err != nil {
			return launcher.Session{}, false, err
		}
		argv = append(argv, "--mcp-config", mcp)
	}
	argv = append(argv, "--permission-mode", "auto")
	if resume {
		argv = append(argv, "--continue")
	}
	argv = append(argv, startPrompt(skill, key, resume))
	return launcher.Session{Dir: dir, Argv: argv, Env: env, PathFirst: pathFirst}, resume, nil
}

func sessionPrompt(skill, key string) string { return "/tickets:" + skill + " " + key }

// continueNudge is what a continued work-ticket session is sent: its
// conversation already holds the playbook, so resending the skill would load
// a second copy. Plain words (no quotes, backticks, # or ;) so a wrapper
// passing it through a shell keeps it unchanged. %s is the ticket ID.
const continueNudge = "ct start --continue: a new process, so agents of the last run are gone. " +
	"Redo step 0 of /tickets:work-ticket %s from board.md and the Open part of relay.md, then carry on. " +
	"If that playbook is no longer in your context, run /tickets:work-ticket %s."

// startPrompt is the session's first prompt: the nudge when a work-ticket
// session really continues, else the skill's own command.
func startPrompt(skill, key string, resume bool) string {
	if resume && skill == "work-ticket" {
		return fmt.Sprintf(continueNudge, key, key)
	}
	return sessionPrompt(skill, key)
}

// runForeground runs the session in this terminal: ct becomes claude
// (Windows: waits for it), and the session's process is recorded so other
// commands see it running.
func runForeground(key string, w launcher.Session, resume bool, o startOpts) error {
	// A missing claude fails before anything is said or recorded
	if _, err := exec.LookPath(w.Argv[0]); err != nil {
		return err
	}
	command := "start"
	if o.feedback {
		command = "feedback"
	}
	// Checked again under the record's lock: another ct start of this ticket
	// may have passed refuseRunning while this one fetched and built the graph
	if err := workspace.ClaimSession(w.Dir, command); err != nil {
		var running *workspace.RunningError
		if errors.As(err, &running) {
			return runningPIDError(key, running.Record.PID, running.Record.Started, o)
		}
		warnf("couldn't record the session in %s: %v", workspace.SessionsFile(w.Dir), err)
	}
	cont := ""
	if resume {
		cont = " (continuing its last session)"
	}
	fmt.Printf("ct start: %s runs in this terminal%s; ct start %s later continues it\n", key, cont, key)
	return passExit(launcher.Run(w))
}

// writeTicketClaudeMD writes the workspace dir's CLAUDE.md from the vault.
func writeTicketClaudeMD(ctx vault.Context, key, dir string) error {
	return os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(ticketClaudeMD(ctx, key, dir)), 0o644)
}

func ticketClaudeMD(ctx vault.Context, key, dir string) string {
	v, p := ctx.Vault, ctx.ProjectsRoot
	ticket := note.TicketPath(v, key)
	branchText, gitSubject := "feature/<description>", manualSubjectRule(ticketCommitStyle(ctx, key))
	if ticketSynced(ctx, key) {
		branchText = "feature/" + key + "[-<description>]"
		gitSubject = "subject starting with `" + key + "`, unless the repo's own convention says otherwise"
	}
	return fmt.Sprintf("# Ticket workspace: %[1]s\n\n"+
		"Written by ct start; regenerated on every start, so don't edit it.\n\n"+
		"- Ticket: `%[1]s` (source: `%[2]s`)\n"+
		"- Vault: `%[3]s` (read its `AGENTS.md` for note rules)\n"+
		"- Ticket note: `%[4]s`\n"+
		"- Ticket folder (the only place in the vault this session writes to before\n"+
		"  `/tickets:save`): `%[9]s`. A lead ticket (`ticket-type: epic`)\n"+
		"  may also write the work sections and `status` of the tickets in its\n"+
		"  `covers`.\n"+
		"- Repos in this workspace: `%[5]s/workspace.json` (each repo's path, base\n"+
		"  and branch), kept current by `ct ws add`. Checkouts are at\n"+
		"  `%[5]s/<slug>`, normally on `%[7]s`.\n"+
		"- Main clones: `%[6]s/<provider>/<owner>/<repo>`. Never edit\n"+
		"  them: their `.git` is only writable so `ct ws add` can create\n"+
		"  worktrees. Each\n"+
		"  repo's slug is `<provider>-<owner>-<repo>` (`ct ws repos` lists\n"+
		"  them); the vault and the code graph name repos by slug.\n"+
		"- Vault tools: `ct vault lock` and `ct vault links` (used by `/tickets:save`).\n"+
		"  `ct new` files a manual ticket.\n"+
		"- Code graph: the `graphify` MCP server merges this workspace's worktrees\n"+
		"  with every other repo's main clone. Query it before reading files.\n"+
		"- Go: `GOPATH`, `GOMODCACHE` and `GOCACHE` are under `%[5]s/.cache/go`\n"+
		"  (set in `.claude/settings.json`), so modules are fetched into this\n"+
		"  workspace. graphify never scans `.cache/`, and `.ignore` keeps it out of\n"+
		"  fff and Grep searches: name the path to search it.\n"+
		"- Tools: `go`, `gofmt` and `python3` come from `.tool-versions` (asdf), pinned\n"+
		"  by ct, so don't export `ASDF_*_VERSION`.\n\n"+
		"- Git: commit your work on the ticket branch in coherent steps (%[8]s).\n"+
		"  Vault-only IDs (task numbers, waves, R-/B- entries, review findings) never\n"+
		"  go into code, comments, docs or commit messages.\n"+
		"  Commits are signed with the session's signing key when one is set up\n"+
		"  (unsigned ones are re-signed by the user on the host). Push only\n"+
		"  through `/tickets:save`, which pushes each repo's ticket branch; never\n"+
		"  force push, delete branches or tags, rewrite pushed commits, reset\n"+
		"  --hard, or remove the ticket worktrees (those are the user's steps);\n"+
		"  the lead adds and removes task worktrees under `tasks/`.\n"+
		"- Workflow: this session is the PO, the one the user talks to. It runs one\n"+
		"  `tickets:lead` subagent, which plans the ticket in waves of task agents\n"+
		"  (each in its own task worktree and branch, merged into the ticket branch\n"+
		"  and reviewed after each wave), and spawns every agent the lead asks for.\n"+
		"  Requests, questions and answers between the user, the PO and the lead go\n"+
		"  through `relay.md`; `board.md` tracks the tasks. When the context is\n"+
		"  heavy, `/tickets:po-refresh` checkpoints them and gives the `/compact` to run.\n\n"+
		"Run `/tickets:work-ticket %[1]s` to (re)start the workflow (`ct start %[1]s`\n"+
		"opens a new conversation, which resumes from `board.md`; `ct start\n"+
		"--continue %[1]s` reopens the last one), and `/tickets:save` once the user\n"+
		"has reviewed the changes: it pushes the ticket branches.\n",
		key, note.Get(ticket, "source"), v, ticket, dir, p, branchText, gitSubject,
		note.TicketDir(v, key)+string(filepath.Separator))
}

// ticketSynced reports whether the ticket's note is a synced ticket's (a
// missing note is a manual ticket).
func ticketSynced(ctx vault.Context, key string) bool {
	return note.Synced(note.Frontmatter(note.TicketPath(ctx.Vault, key)))
}

// ticketCommitStyle is the commit-style the ticket's board.md names, "" when
// the file or the field is missing or the value is not a style a manual
// ticket may use.
func ticketCommitStyle(ctx vault.Context, key string) string {
	st := note.Frontmatter(filepath.Join(note.TicketDir(ctx.Vault, key), "board.md"))["commit-style"]
	if st == "conventional" || st == "repo" {
		return st
	}
	return ""
}

// manualSubjectRule is the commit-subject rule of a manual ticket in style.
func manualSubjectRule(style string) string {
	rule := "not chosen yet (the kickoff asks): once the ticket's board.md has `commit-style:`, follow it; until then a plain imperative summary"
	switch style {
	case "conventional":
		rule = "conventional commits"
	case "repo":
		rule = "the repo's own convention, without an ID"
	}
	return "subjects never carry the ticket ID or any vault-only ID; style: " + rule
}

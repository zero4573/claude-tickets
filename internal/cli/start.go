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

	"github.com/zero4573/tix/internal/harness"
	"github.com/zero4573/tix/internal/launcher"
	"github.com/zero4573/tix/internal/modelcache"
	"github.com/zero4573/tix/internal/note"
	"github.com/zero4573/tix/internal/vault"
	"github.com/zero4573/tix/internal/workspace"
)

type startOpts struct {
	force, noFetch, feedback, cont, list, all bool
	// agent is --agent: the CLI the session runs (claude or kiro), else
	// $TIX_AGENT, else the vault's .workflow.json "agent"
	agent string
	// dev is --dev (a checkout slug, or none)
	dev string
	// name is the command as typed (tix start, tix feedback), for messages
	name string
}

// isTerminal: stdin and stdout are a terminal (a session can run here).
var isTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) }

// AssumeTerminal makes tix treat stdin and stdout as a terminal. For tests
// only: the tix-tty command of the CLI tests.
func AssumeTerminal() {
	isTerminal = func() bool { return true }
}

const forceHelp = "start tickets marked ignore: true too, one covered by an open lead ticket on its own, or one whose session only looks running from its .agent-state"
const continueHelp = "continue the workspace's last conversation instead of starting a new one"
const agentHelp = "the agent CLI the session runs: claude or kiro (default: $TIX_AGENT, else the vault's .workflow.json \"agent\", else claude)"

// foregroundHelp is the part of tix start's and tix feedback's help about
// where the session runs.
const foregroundHelp = `The session runs in this terminal, in the foreground (tix becomes claude):
one ticket per run, from a terminal. A ticket whose session is running (its
process is alive on this host) isn't started again; one whose session only
looks running (its .agent-state is less than a day old and not exited, e.g.
after its terminal was closed) needs --force.`

func startCmd() *cobra.Command {
	o := startOpts{name: "tix start"}
	cmd := &cobra.Command{
		Use:   "start [--agent claude|kiro] [--force] [--no-fetch] [--feedback] [--continue] [--dev <slug>|none] <ID> | --list [--all]",
		Short: "Start (or re-open) a ticket's session in this terminal",
		Long: `Starts (or re-opens) a Claude session for the ticket named, from the
current vault (tix vault default), in this terminal, running
/tix:work-ticket <ID> in the workspace <workRoot>/<ID> (by default
~/Projects/work-<vault>/<ID>; see tix vault). It must be a ticket of the
vault: tickets/<source>/<ID>/<ID>.md, synced by tix sync or a manual ticket
from tix new.

Each session is a new conversation, even in a workspace that already ran
one: /tix:work-ticket resumes from the ticket's board.md, so a fresh
conversation every wave or two keeps the context small. --continue (-c)
continues the workspace's last conversation instead. The workspace is marked as trusted in Claude Code, so the session starts
without asking. tix status shows every ticket's session.

` + foregroundHelp + `

--feedback runs /tix:pr-feedback <ID> instead: apply the review feedback
on your open PRs for the ticket (tix feedback is a shorthand). For a ticket
whose session is running, type /tix:pr-feedback in its terminal.

--dev <slug> makes the workspace a dev workspace, for a ticket that changes
tix itself: at each start tix builds tix from the workspace's
checkout <slug> (go build, into <workspace>/.cache/dev) and copies its
plugin there, and its sessions run that tix (first on PATH) and that plugin
instead of the installed ones. tix never runs the
build itself; a running session keeps the build it started with, and tix clean
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
				return errors.New("name at least one ticket (tix start --list shows them)")
			}
			return start(ctx, args, o)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&o.force, "force", false, forceHelp)
	f.BoolVar(&o.noFetch, "no-fetch", false, "skip fetching the main clones first")
	f.BoolVar(&o.feedback, "feedback", false, "run /tix:pr-feedback instead of /tix:work-ticket")
	f.BoolVarP(&o.cont, "continue", "c", false, continueHelp)
	f.BoolVar(&o.list, "list", false, "print the vault's open tickets")
	f.BoolVar(&o.all, "all", false, "with --list: done and closed tickets too")
	f.StringVar(&o.dev, "dev", "", devHelp)
	f.StringVar(&o.agent, "agent", "", agentHelp)
	return cmd
}

func feedbackCmd() *cobra.Command {
	o := startOpts{name: "tix feedback"}
	cmd := &cobra.Command{
		Use:   "feedback [--agent claude|kiro] [--force] [--no-fetch] [--continue] <ID>",
		Short: "tix start --feedback: apply the review feedback on your open PRs",
		Long: `Starts a session for the ticket named running /tix:pr-feedback <ID>:
it applies the review feedback on your open PRs for the ticket, as tix start
--feedback does. Each session is a new conversation; --continue (-c)
continues the workspace's last conversation instead.

` + foregroundHelp + ` A running session gets no keys typed into it: type
/tix:pr-feedback in its terminal yourself.`,
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
	f.StringVar(&o.agent, "agent", "", agentHelp)
	return cmd
}

// completeOneID completes a single ticket ID: tix start and tix feedback take one.
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
		return fmt.Errorf("not a ticket of the %s vault: %s (no tickets/<source>/<ID>/<ID>.md; tix sync, or tix new for a manual ticket; tix start --list shows them); nothing started",
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
					warnf("%s: covered by %s, so its PRs are handled there: tix feedback %s (or --force)", k, lead, lead)
				} else {
					warnf("%s: covered by %s, so it's worked there: tix start %s (or --force to start it on its own)", k, lead, lead)
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
	h, err := harness.Select(o.agent, vault.AgentOf(ctx.Vault))
	if err != nil {
		return err
	}

	k := launch[0]
	w, resume, err := prepareSession(ctx, k, skill, h, o.cont, devRebuild)
	if err != nil {
		return err
	}
	return runForeground(k, w, resume, o)
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
		if o.feedback && o.name == "tix start" {
			force = "tix start --feedback --force " + key
		}
		if o.feedback {
			return fmt.Errorf("%s: its session looks running (.agent-state: %s at %s); type /tix:pr-feedback in that session's terminal, or if it isn't running, %s",
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
		return fmt.Errorf("%s: its session is running (pid %d) in another terminal; type /tix:pr-feedback there", key, pid)
	}
	started := ""
	if t, err := time.Parse(time.RFC3339, startedAt); err == nil {
		started = ", started " + t.Format("15:04")
	}
	return fmt.Errorf("%s: its session is running (pid %d%s) in another terminal; continue there, or end it first", key, pid, started)
}

// ticketSession is a ticket's session in its workspace dir: the vault and
// the repo directory read-write (tix ws clone and tix ws add's worktrees:
// one folder, not one per repo, which would grow every session's context),
// the embedding model cache read-only (each only when it exists), and the
// workspace's instructions.
func ticketSession(ctx vault.Context, key, dir string) harness.Session {
	return harness.Session{
		Dir:          dir,
		Dirs:         []string{ctx.Vault, ctx.ProjectsRoot},
		ReadOnly:     []string{modelcache.Dir()},
		Instructions: ticketClaudeMD(ctx, key, dir),
		Settings:     true,
	}
}

// prepareTicketSession readies a ticket's workspace for a session of h (its
// workspace.json, tool versions, and h's files: settings, trust and
// CLAUDE.md for claude) and returns the session (no prompt yet) and, in a
// dev workspace (tix start --dev; devMode says what to do with its build),
// the build.
func prepareTicketSession(ctx vault.Context, key string, h harness.Harness, mode devMode) (harness.Session, *devBuild, error) {
	dir := filepath.Join(ctx.WorkRoot, key)
	if err := workspace.Ensure(dir, key, ctx.Vault); err != nil {
		return harness.Session{}, nil, err
	}
	dev, err := prepareDev(dir, mode)
	if err != nil {
		return harness.Session{}, nil, err
	}
	s := ticketSession(ctx, key, dir)
	if dev != nil {
		s.Plugin, s.Hooks = dev.plugin, dev.tix
	}
	writeToolVersions(ctx, dir)
	if err := h.Prepare(s); err != nil {
		return harness.Session{}, nil, err
	}
	return s, dev, nil
}

// prepareSession readies a ticket's workspace (prepareTicketSession) and
// builds its session: a new conversation, or with cont (--continue) the
// workspace's last one (resume) when it already ran one, with the same
// agent CLI.
func prepareSession(ctx vault.Context, key, skill string, h harness.Harness, cont bool, mode devMode) (launcher.Session, bool, error) {
	dir := filepath.Join(ctx.WorkRoot, key)
	_, err := os.Stat(filepath.Join(dir, ".agent-state"))
	resume := err == nil && cont
	if cont && !resume {
		warnf("%s: no earlier session in its workspace to continue, starting a new one", key)
	}
	if prev := workspace.Agent(dir); resume && prev != h.Name() {
		warnf("%s: its last session ran %s, not %s, so it can't be continued: starting a new one", key, prev, h.Name())
		resume = false
	}
	s, dev, err := prepareTicketSession(ctx, key, h, mode)
	if err != nil {
		return launcher.Session{}, false, err
	}
	if err := workspace.SetAgent(dir, h.Name()); err != nil {
		warnf("couldn't record the agent in %s: %v", workspace.File(dir), err)
	}
	env := h.Env(s)
	env["TIX_VAULT"] = ctx.Vault
	pathFirst := ""
	if dev != nil {
		pathFirst = dev.bin
		// For a wrapper (sandbox, container) that sets its own PATH: it
		// prepends this to have the built tix first
		env["TIX_DEV_BIN"] = dev.bin
	}
	s.Continue = resume
	s.Prompt = startPrompt(h, skill, key, resume)
	argv, err := h.Argv(s)
	if err != nil {
		return launcher.Session{}, false, err
	}
	return launcher.Session{Dir: dir, Argv: argv, Env: env, PathFirst: pathFirst}, resume, nil
}

func sessionPrompt(h harness.Harness, skill, key string) string { return h.SkillRef(skill) + " " + key }

// continueNudge is what a continued work-ticket session is sent: its
// conversation already holds the playbook, so resending the skill would load
// a second copy. Plain words (no quotes, backticks, # or ;) so a wrapper
// passing it through a shell keeps it unchanged. %[1]s is the skill command
// (/tix:work-ticket), %[2]s the ticket ID.
const continueNudge = "tix start --continue: a new process, so agents of the last run are gone. " +
	"Redo step 0 of %[1]s %[2]s from board.md and the Open part of relay.md, then carry on. " +
	"If that playbook is no longer in your context, run %[1]s %[2]s."

// startPrompt is the session's first prompt: the nudge when a work-ticket
// session really continues, else the skill's own command.
func startPrompt(h harness.Harness, skill, key string, resume bool) string {
	if resume && skill == "work-ticket" {
		return fmt.Sprintf(continueNudge, h.SkillRef("work-ticket"), key)
	}
	return sessionPrompt(h, skill, key)
}

// runForeground runs the session in this terminal: tix becomes claude
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
	// Checked again under the record's lock: another tix start of this ticket
	// may have passed refuseRunning while this one fetched
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
	fmt.Printf("tix start: %s runs in this terminal%s; tix start %s later continues it\n", key, cont, key)
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
		"Written by tix start; regenerated on every start, so don't edit it.\n\n"+
		"- Ticket: `%[1]s` (source: `%[2]s`)\n"+
		"- Vault: `%[3]s` (read its `AGENTS.md` for note rules)\n"+
		"- Ticket note: `%[4]s`\n"+
		"- Ticket folder (the only place in the vault this session writes to before\n"+
		"  `/tix:save`): `%[9]s`. A lead ticket (`ticket-type: epic`)\n"+
		"  may also write the work sections and `status` of the tickets in its\n"+
		"  `covers`.\n"+
		"- Repos in this workspace: `%[5]s/workspace.json` (each repo's path, base\n"+
		"  and branch), kept current by `tix ws add`. Checkouts are at\n"+
		"  `%[5]s/<slug>`, normally on `%[7]s`.\n"+
		"- Main clones: `%[6]s/<provider>/<owner>/<repo>`. Never edit\n"+
		"  them: their `.git` is only writable so `tix ws add` can create\n"+
		"  worktrees. Each\n"+
		"  repo's slug is `<provider>-<owner>-<repo>` (`tix ws repos` lists\n"+
		"  them); the vault names repos by slug.\n"+
		"- Vault tools: `tix vault lock` and `tix vault links` (used by `/tix:save`).\n"+
		"  `tix new` files a manual ticket.\n"+
		"- Go: `GOPATH`, `GOMODCACHE` and `GOCACHE` are under `%[5]s/.cache/go`\n"+
		"  (set in `.claude/settings.json`), so modules are fetched into this\n"+
		"  workspace. `.ignore` keeps it out of Grep searches: name the path to\n"+
		"  search it.\n"+
		"- Tools: `go`, `gofmt` and `python3` come from `.tool-versions` (asdf), pinned\n"+
		"  by tix, so don't export `ASDF_*_VERSION`.\n\n"+
		"- Git: commit your work on the ticket branch in coherent steps (%[8]s).\n"+
		"  Vault-only IDs (task numbers, waves, R-/B- entries, review findings) never\n"+
		"  go into code, comments, docs or commit messages.\n"+
		"  Commits are signed with the session's signing key when one is set up\n"+
		"  (unsigned ones are re-signed by the user on the host). Push only\n"+
		"  through `/tix:save`, which pushes each repo's ticket branch; never\n"+
		"  force push, delete branches or tags, rewrite pushed commits, reset\n"+
		"  --hard, or remove the ticket worktrees (those are the user's steps);\n"+
		"  the lead adds and removes task worktrees under `tasks/`.\n"+
		"- Workflow: this session is the PO, the one the user talks to. It runs one\n"+
		"  `tix:lead` subagent, which plans the ticket in waves of task agents\n"+
		"  (each in its own task worktree and branch, merged into the ticket branch\n"+
		"  and reviewed after each wave), and spawns every agent the lead asks for.\n"+
		"  Requests, questions and answers between the user, the PO and the lead go\n"+
		"  through `relay.md`; `board.md` tracks the tasks. When the context is\n"+
		"  heavy, `/tix:po-refresh` checkpoints them and gives the `/compact` to run.\n\n"+
		"Run `/tix:work-ticket %[1]s` to (re)start the workflow (`tix start %[1]s`\n"+
		"opens a new conversation, which resumes from `board.md`; `tix start\n"+
		"--continue %[1]s` reopens the last one), and `/tix:save` once the user\n"+
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

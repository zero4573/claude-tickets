package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/platform"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

// completeIDs offers IDs (with their summary and status) from list,
// skipping the ones already on the command line.
func completeIDs(list func(vault.Context) []note.Ticket) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		ctx, err := vault.Require()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		given := map[string]bool{}
		for _, a := range args {
			given[a] = true
		}
		var out []string
		for _, t := range list(ctx) {
			if !given[t.ID] {
				out = append(out, fmt.Sprintf("%s\t%s [%s]", t.ID, t.Summary, t.Status))
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

func openTickets(ctx vault.Context) []note.Ticket { return note.List(ctx.Vault, false) }

func workspaceTickets(ctx vault.Context) []note.Ticket {
	var out []note.Ticket
	for _, t := range note.List(ctx.Vault, true) {
		if _, err := os.Stat(workspace.File(filepath.Join(ctx.WorkRoot, t.ID))); err == nil {
			out = append(out, t)
		}
	}
	return out
}

func printTickets(ts []note.Ticket) {
	for _, t := range ts {
		fmt.Printf("%s\t%s\t%s\n", t.ID, t.Status, t.Summary)
	}
}

func newCmd() *cobra.Command {
	var typ, priority string
	var open bool
	cmd := &cobra.Command{
		Use:   `new [--type dev|bug|investigation|chore|epic] [--priority <p>] [--open] "<summary>"`,
		Short: "Create a manual ticket",
		Long: `Creates a manual ticket in the current vault (ct vault default): one you
write and manage in Obsidian, never synced from a remote source. It takes
the next free ID (<prefix>-<n>, prefix from the manual source's idPrefix in
tickets/.sources.json, MAN by default), creates tickets/manual/<ID>/<ID>.md from
templates/ticket-manual.md, and prints its path. Fill in its Description
and Acceptance criteria, then run ct start <ID>: manual tickets go
through the same agent workflow as synced ones.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch typ {
			case "dev", "bug", "investigation", "chore", "epic":
			default:
				return errors.New("--type must be dev, bug, investigation, chore or epic")
			}
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			path, err := newTicket(ctx.Vault, args[0], typ, priority)
			if err != nil {
				return err
			}
			refreshIndex(ctx.Vault, relTo(ctx.Vault, path))
			fmt.Println(path)
			if open {
				link := "obsidian://open?path=" + strings.ReplaceAll(url.PathEscape(path), "/", "%2F")
				argv := platform.OpenCommand(link)
				if err := exec.Command(argv[0], argv[1:]...).Run(); err != nil {
					fmt.Fprintf(os.Stderr, "ct: couldn't open Obsidian; open %s yourself\n", path)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&typ, "type", "dev", "ticket-type, which shapes the lead's plan (epic makes a lead ticket that covers others)")
	cmd.Flags().StringVar(&priority, "priority", "", "free text, e.g. high")
	cmd.Flags().BoolVar(&open, "open", false, "open the new note in Obsidian")
	return cmd
}

func yamlQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func newTicket(vaultDir, summary, typ, priority string) (string, error) {
	template := filepath.Join(vaultDir, "templates", "ticket-manual.md")
	tmpl, err := os.ReadFile(template)
	if err != nil {
		return "", fmt.Errorf("no template at %s", template)
	}
	prefix := "MAN"
	if data, err := os.ReadFile(filepath.Join(vaultDir, "tickets", ".sources.json")); err == nil {
		var cfg struct {
			Sources struct {
				Manual struct {
					Enabled  *bool  `json:"enabled"`
					IDPrefix string `json:"idPrefix"`
				} `json:"manual"`
			} `json:"sources"`
		}
		if json.Unmarshal(data, &cfg) == nil {
			if cfg.Sources.Manual.IDPrefix != "" {
				prefix = cfg.Sources.Manual.IDPrefix
			}
			if e := cfg.Sources.Manual.Enabled; e != nil && !*e {
				return "", errors.New("the manual source is disabled in tickets/.sources.json")
			}
		}
	}
	if !note.ValidKey(prefix + "-1") {
		return "", fmt.Errorf("manual idPrefix '%s' doesn't make valid ticket IDs", prefix)
	}
	tickets := note.SourceDir(vaultDir, "manual")
	if err := os.MkdirAll(tickets, 0o755); err != nil {
		return "", err
	}
	// Next free number, past every ticket folder's (any source, an old
	// vault's tickets/<ID>/) and the archived tickets' too (ct clean): a
	// reused ID would give two notes one name. mkdir is atomic, so two
	// concurrent runs can't take the same ID
	n := 0
	names := note.ArchivedIDs(vaultDir)
	for _, l := range note.TicketDirs(vaultDir) {
		names = append(names, l.ID)
	}
	for _, name := range names {
		if num, ok := strings.CutPrefix(name, prefix+"-"); ok {
			if v, err := strconv.Atoi(num); err == nil && v > n {
				n = v
			}
		}
	}
	var id string
	for {
		n++
		id = fmt.Sprintf("%s-%d", prefix, n)
		if note.Find(vaultDir, id) != "" {
			continue
		}
		if err := os.Mkdir(filepath.Join(tickets, id), 0o755); err == nil {
			break
		} else if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	today := time.Now().Format("2006-01-02")
	var out strings.Builder
	done := map[string]bool{}
	yamlPriority := ""
	if priority != "" {
		yamlPriority = yamlQuote(priority)
	}
	for _, line := range strings.SplitAfter(string(tmpl), "\n") {
		nl := ""
		if strings.HasSuffix(line, "\n") {
			line, nl = strings.TrimSuffix(line, "\n"), "\n"
		}
		line = strings.ReplaceAll(strings.ReplaceAll(line, "{{title}}", id), "{{date}}", today)
		switch {
		case strings.HasPrefix(line, "summary:") && !done["summary"]:
			line, done["summary"] = "summary: "+yamlQuote(summary), true
		case strings.HasPrefix(line, "ticket-type:") && !done["type"]:
			line, done["type"] = "ticket-type: "+typ, true
		case strings.HasPrefix(line, "priority:") && !done["priority"]:
			line, done["priority"] = "priority: "+yamlPriority, true
		case strings.HasPrefix(line, "# ") && !done["heading"]:
			line, done["heading"] = "# "+id+": "+summary, true
		}
		out.WriteString(line + nl)
	}
	path := note.SourcePath(vaultDir, "manual", id)
	return path, os.WriteFile(path, []byte(out.String()), 0o644)
}

func statusCmd() *cobra.Command {
	var watchMode bool
	interval := intervalFlag(2 * time.Second)
	cmd := &cobra.Command{
		Use:   "status [-w] [--interval <duration>]",
		Short: "Every ticket session of the vault, waiting on you first",
		Long: `One row per ticket workspace of the current vault, tickets waiting on you
first:
  AGENT   what the session is doing, from its hooks (.agent-state):
          needs-input (a question or permission prompt is waiting),
          idle (finished its turn, waiting for your next message),
          working, exited; "stale" when its process has ended without
          the session having ended
  SOURCE  where the ticket comes from (jira, manual, ...)
  STATUS  the ticket note's status in the vault
  REPOS   worktrees, and how many have uncommitted changes

A session runs in the terminal where it was started. It counts as running
when its process (recorded by ct start) is alive on this host, else, when
that can't be checked, when its .agent-state is less than a day old and not
exited.

With --watch (-w), ct status stays on screen and redraws the table until
Ctrl-C: every --interval (a duration such as 2s or 1m; default 2s, at least
1s) and within a second of any session's .agent-state changing. The first
line says when the table was last refreshed; rows that don't fit the
terminal are summed up as "... N more". When stdout isn't a terminal it
prints the table once, as without -w, with a warning on stderr.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("interval") && !watchMode {
				return errIntervalNeedsWatch
			}
			// Resolved once: watch mode keeps this vault for its whole run
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			switch {
			case !watchMode:
				return writeStatus(os.Stdout, buildStatus(ctx))
			case !stdoutIsTerminal():
				return printStatusOnce(ctx, "a terminal")
			}
			return runStatusWatch(cmd.Context(), ctx, time.Duration(interval))
		},
	}
	cmd.Flags().BoolVarP(&watchMode, "watch", "w", false, "keep the table on screen and refresh it until Ctrl-C")
	cmd.Flags().Var(&interval, "interval", "how often --watch refreshes (at least 1s)")
	return cmd
}

// statusView is what ct status shows: the header line, then the rows or,
// without any, the empty line. Plain and watch mode both render it.
type statusView struct {
	header string
	rows   []statusRow
	empty  string
}

func buildStatus(ctx vault.Context) statusView {
	v := statusView{
		header: fmt.Sprintf("Vault %s (sessions run in their own terminal)", filepath.Base(ctx.Vault)),
		rows:   statusRows(ctx),
	}
	if len(v.rows) == 0 {
		v.empty = fmt.Sprintf("No ticket workspaces under %s (start one with ct start <ID>).", ctx.WorkRoot)
	}
	return v
}

// writeStatus writes the header, then the table (or the empty line).
func writeStatus(out io.Writer, v statusView) error {
	if _, err := fmt.Fprintln(out, v.header); err != nil {
		return err
	}
	if len(v.rows) == 0 {
		_, err := fmt.Fprintln(out, v.empty)
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TICKET\tSOURCE\tAGENT\tSTATUS\tREPOS\tSINCE\tWAITING ON")
	for _, r := range v.rows {
		fmt.Fprintln(w, strings.Join(r.cols, "\t"))
	}
	return w.Flush()
}

type statusRow struct {
	order int
	key   string
	cols  []string
}

var vaultLine = regexp.MustCompile("(?m)^- Vault: `([^`]*)`")

func statusRows(ctx vault.Context) []statusRow {
	var rows []statusRow
	for _, dir := range workspace.List(ctx.WorkRoot, false) {
		key := filepath.Base(dir)
		st := workspace.ReadState(dir)
		state := st.State
		if state == "" {
			state = "-"
		}
		if state != "exited" && state != "-" && !workspace.Running(dir) {
			state += " (stale)"
		}
		info, _ := workspace.Read(dir)
		v := info.Vault
		if v == "" {
			// Workspaces from before workspace.json recorded the vault
			if data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md")); err == nil {
				if m := vaultLine.FindSubmatch(data); m != nil {
					v = string(m[1])
				}
			}
		}
		vaultStatus, source := "-", "-"
		if v != "" {
			fm := note.Frontmatter(note.TicketPath(v, key))
			if fm["status"] != "" {
				vaultStatus = fm["status"]
			}
			if fm["source"] != "" {
				source = fm["source"]
			}
		}
		total, dirty := 0, 0
		for _, r := range info.Repos {
			if _, err := os.Stat(filepath.Join(r.Path, ".git")); err != nil {
				continue
			}
			total++
			if out, _ := exec.Command("git", "-C", r.Path, "status", "--porcelain").Output(); len(out) > 0 {
				dirty++
			}
		}
		order := 3
		switch {
		case strings.HasPrefix(state, "needs-input"):
			order = 0
		case strings.HasPrefix(state, "idle"):
			order = 1
		case strings.HasPrefix(state, "working"):
			order = 2
		}
		reason := st.Reason
		if len([]rune(reason)) > 60 {
			reason = string([]rune(reason)[:57]) + "..."
		}
		since := ""
		if len(st.TS) >= 19 {
			since = st.TS[11:19]
		}
		rows = append(rows, statusRow{order, key, []string{
			key, source, state, vaultStatus, fmt.Sprintf("%d (%d dirty)", total, dirty), since, reason,
		}})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].order != rows[j].order {
			return rows[i].order < rows[j].order
		}
		return rows[i].key < rows[j].key
	})
	return rows
}

func openCmd() *cobra.Command {
	var list bool
	cmd := &cobra.Command{
		Use:   "open <ID> | --list",
		Short: "Open a ticket's workspace in your editor",
		Long: `Opens a ticket's workspace in your editor: $CLAUDE_TICKETS_EDITOR, a command
with any arguments (default: code, VS Code).

For VS Code and its forks (code, codium, cursor, ...), it writes and opens
<ID>.code-workspace in the workspace: a multi-root workspace with one folder
per worktree (named <slug> (<branch>)) plus the ticket's notes in the vault,
so each repo gets its own source control. Rewritten on every run, from
workspace.json. Other editors get the workspace folder.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeIDs(workspaceTickets),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			if list {
				printTickets(workspaceTickets(ctx))
				return nil
			}
			if len(args) != 1 {
				return cmd.Usage()
			}
			return openWorkspace(ctx, args[0])
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "print the tickets that have a workspace")
	return cmd
}

func openWorkspace(ctx vault.Context, key string) error {
	if !note.ValidKey(key) {
		return fmt.Errorf("not a ticket ID: '%s'", key)
	}
	dir := filepath.Join(ctx.WorkRoot, key)
	info, err := workspace.Read(dir)
	if err != nil {
		return fmt.Errorf("%s has no workspace in the %s vault (%s): start it with ct start %s", key, filepath.Base(ctx.Vault), dir, key)
	}
	editor := strings.Fields(os.Getenv("CLAUDE_TICKETS_EDITOR"))
	if len(editor) == 0 {
		editor = []string{"code"}
	}
	path, err := exec.LookPath(editor[0])
	if err != nil {
		return fmt.Errorf("editor '%s' not found (set CLAUDE_TICKETS_EDITOR)", editor[0])
	}
	target := dir
	switch filepath.Base(editor[0]) {
	case "code", "code-insiders", "codium", "vscodium", "cursor", "windsurf":
		type folder struct {
			Name string `json:"name"`
			Path string `json:"path"`
		}
		var folders []folder
		for _, r := range info.Repos {
			folders = append(folders, folder{fmt.Sprintf("%s (%s)", r.Slug, r.Branch), r.Path})
		}
		folders = append(folders, folder{key + " notes (vault)", note.TicketDir(ctx.Vault, key)})
		data, _ := json.MarshalIndent(map[string]any{"folders": folders}, "", "  ")
		target = filepath.Join(dir, key+".code-workspace")
		if err := os.WriteFile(target, append(data, '\n'), 0o644); err != nil {
			return err
		}
	}
	return execReplaceOrRun(path, append(editor, target))
}

// execReplaceOrRun replaces ct with argv (path is argv[0] resolved), or
// runs it where exec isn't possible.
func execReplaceOrRun(path string, argv []string) error {
	if err := execReplace(path, argv, os.Environ()); err != nil {
		cmd := exec.Command(path, argv[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
	return nil
}

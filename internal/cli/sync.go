package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zero4573/tix/internal/config"
	"github.com/zero4573/tix/internal/followup"
	"github.com/zero4573/tix/internal/jira"
	"github.com/zero4573/tix/internal/mcp"
	"github.com/zero4573/tix/internal/platform"
	"github.com/zero4573/tix/internal/session"
	"github.com/zero4573/tix/internal/vault"
	"github.com/zero4573/tix/internal/workspace"
)

func syncCmd() *cobra.Command {
	var only string
	var full bool
	cmd := &cobra.Command{
		Use:   "sync [--source <name>] [--full]",
		Short: "Pull your open tickets from each source into the vault",
		Long: `Pulls your open tickets from every enabled, syncable source in the current
vault's tickets/.sources.json (e.g. jira) into tickets/<source>/<ID>/<ID>.md, and
keeps the local notes reconciled with the source: new and changed tickets
are written, tickets the source closed are closed, reassigned or deleted
ones are tagged unassigned, epics are refreshed when their children
change, and blocked follows the blockers' status. Sources with
"sync": false (e.g. manual tickets, which you write in Obsidian) are
skipped.

For jira, tix sync syncs by itself, talking to the source's MCP server (its
URL from $TIX_MCP_CONFIG; no model, no tokens). It lists the open
tickets with their last-updated time, compares them with the notes'
frontmatter, and looks up the rest by key. For each ticket that moved,
Jira's changelog says what changed, and only that part of the note is
rewritten: the table and frontmatter for field changes, the description,
or the recent comments. Changes the note doesn't show (time tracking, rank,
sprint, ...) only move the timestamp. Claude (headless,
$TIX_CLAUDE, the ticket-sync skill, with the source's "model" in
.sources.json, default sonnet) is started only for a description or
comments that Jira can only give as HTML. Other sources run the skill over
everything.

Progress is shown as it happens and logged to
` + logPath("ticket-sync-<vault>") + `. Anything left for you to check
(failed sources or tickets, tickets that left you) goes into a follow-up
note in the vault's inbox/, as tasks.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			return ticketSync(ctx, only, full)
		},
	}
	cmd.Flags().StringVar(&only, "source", "", "sync only this source")
	cmd.Flags().BoolVar(&full, "full", false, "have Claude go over every open ticket (the skill's own listing), e.g. after changing the note layout")
	return cmd
}

// orderedKeys is the keys of a JSON object in the order they're written.
func orderedKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return keys
		}
		keys = append(keys, t.(string))
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return keys
		}
	}
	return keys
}

type sourceConf struct {
	Enabled    *bool    `json:"enabled"`
	Sync       *bool    `json:"sync"`
	MCP        string   `json:"mcp"`
	Model      string   `json:"model"`
	Site       string   `json:"site"`
	Query      string   `json:"query"`
	TextFields []string `json:"textFields"`
}

func ticketSync(ctx vault.Context, only string, full bool) error {
	wrote := false
	noteRel := ""
	var before map[string]stamp
	var beforeErr error
	cfgFile := filepath.Join(ctx.Vault, "tickets", ".sources.json")
	stateFile := filepath.Join(ctx.Vault, "tickets", ".sync-state.json")
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		return fmt.Errorf("no %s (it lists the ticket sources; see the vault's AGENTS.md)", cfgFile)
	}
	var cfg struct {
		Sources json.RawMessage `json:"sources"`
	}
	if json.Unmarshal(data, &cfg) != nil || !bytes.HasPrefix(bytes.TrimSpace(cfg.Sources), []byte("{")) {
		return fmt.Errorf("%s has no \"sources\" object", cfgFile)
	}
	var all map[string]sourceConf
	if err := json.Unmarshal(cfg.Sources, &all); err != nil {
		return fmt.Errorf("%s: %w", cfgFile, err)
	}
	if only != "" {
		sc, ok := all[only]
		if !ok {
			return fmt.Errorf("no source '%s' in %s", only, cfgFile)
		}
		if sc.Sync != nil && !*sc.Sync {
			fmt.Printf("tix sync: '%s' has \"sync\": false (its tickets are managed in the vault); nothing to sync\n", only)
			return nil
		}
	}
	var sources []string
	for _, k := range orderedKeys(cfg.Sources) {
		sc := all[k]
		if (sc.Enabled != nil && !*sc.Enabled) || (sc.Sync != nil && !*sc.Sync) || (only != "" && k != only) {
			continue
		}
		sources = append(sources, k)
	}
	if len(sources) == 0 {
		fmt.Printf("tix sync: no enabled sources to sync in %s\n", cfgFile)
		return nil
	}

	// The skill and tix sync write ticket notes without listing them: snapshot
	// tickets/ here and, after a run that wrote something (a source that ran),
	// refresh the index for exactly the files that changed.
	before, beforeErr = snapshotTickets(ctx.Vault)
	defer func() {
		if wrote {
			refreshAfterSync(ctx.Vault, before, beforeErr, noteRel)
		} else if noteRel != "" {
			// No source wrote anything (e.g. Jira unreachable), but the
			// follow-up note is a new file the index doesn't know yet.
			refreshIndex(ctx.Vault, noteRel)
		}
	}()

	logName := "ticket-sync-" + filepath.Base(ctx.Vault)
	logFile := config.StateLog(logName)
	logf, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	// What each source did, for the notification and the log
	summaryF, err := os.CreateTemp("", "tix-sync-summary.")
	if err != nil {
		return err
	}
	summaryF.Close()
	defer os.Remove(summaryF.Name())
	summary := summaryF.Name()
	appendTo := func(file, line string) {
		if f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintln(f, line)
			f.Close()
		}
	}
	say := func(line string) {
		fmt.Println(line)
		appendTo(summary, line)
	}
	both := func(line string) {
		fmt.Println(line)
		fmt.Fprintln(logf, line)
	}
	var followups []string
	planFile := filepath.Join(ctx.Vault, "tickets", ".sync-plan.json")
	fuFile := filepath.Join(ctx.Vault, "tickets", ".sync-followups")
	defer os.Remove(planFile)

	var failed []string
	// Tickets that couldn't be written (their sources still count as synced)
	var ticketsFailed []string
	for _, src := range sources {
		sc := all[src]
		if sc.MCP == "" {
			warnf("%s: no \"mcp\" server set in %s, skipping", src, cfgFile)
			followups = append(followups, fmt.Sprintf("ticket-sync %s: set the \"mcp\" server for the %s source in tickets/.sources.json", src, src))
			failed = append(failed, src)
			continue
		}
		model := sc.Model
		if model == "" {
			model = "sonnet"
		}
		both(fmt.Sprintf("=== tix sync %s vault=%s source=%s", time.Now().Format(time.RFC3339), ctx.Vault, src))

		claudePrompt := "/tix:ticket-sync " + src + " --followups tickets/.sync-followups"
		runClaude := true
		var js *jira.Sync
		if src == "jira" && !full {
			js = (&jira.Sync{
				Vault: ctx.Vault, Source: src, Site: sc.Site, Query: sc.Query, TextFields: sc.TextFields,
				KnownChildren: knownChildren(stateFile, src), LogPath: config.TildePath(logFile),
				Say: say, Followup: func(t string) { followups = append(followups, t) },
				Log: func(l string) { fmt.Fprintln(os.Stderr, l) },
			})
			c, err := mcp.Connect(sc.MCP)
			if err == nil {
				js.New(c)
				if err = js.Connect(); err == nil {
					err = js.MakePlan()
				}
			}
			if err != nil {
				if !errors.Is(err, jira.ErrReported) {
					fmt.Fprintln(os.Stderr, "tix: "+err.Error())
				}
				say(fmt.Sprintf("%s: couldn't reach Jira through the '%s' MCP server (see the messages above)", src, sc.MCP))
				followups = append(followups, fmt.Sprintf("ticket-sync %s: the run failed before syncing; check %s", src, config.TildePath(logFile)))
				failed = append(failed, src)
				continue
			}
			runClaude = false
			say(js.Plan.String())
			// Each ticket's changes are found and written here; failures become
			// follow-ups and are retried next run (the note's source-updated stays)
			wrote = true
			if bad := js.Apply(); len(bad) > 0 {
				ticketsFailed = append(ticketsFailed, bad...)
			}
			// Only what Jira can give as HTML alone goes to Claude, part by part
			if len(js.HTML) > 0 {
				if err := config.WriteJSON(planFile, struct {
					Source    string          `json:"source"`
					CloudID   string          `json:"cloudId"`
					AccountID string          `json:"accountId"`
					HTML      []jira.HTMLPart `json:"html"`
				}{js.Plan.Source, js.Plan.CloudID, js.Plan.AccountID, js.HTML}); err != nil {
					return err
				}
				claudePrompt = "/tix:ticket-sync " + src + " --plan tickets/.sync-plan.json --followups tickets/.sync-followups"
				runClaude = true
				both(fmt.Sprintf("tix sync: %s: %d HTML-only part(s) go to Claude", src, len(js.HTML)))
			} else {
				both(fmt.Sprintf("tix sync: %s: written without Claude", src))
			}
		}

		if runClaude {
			_ = os.Remove(fuFile)
			// --strict-mcp-config: only the MCP servers given on the command line
			// (by $TIX_CLAUDE, or whatever wraps claude), not the
			// claude.ai connectors tied to the login, which need interactive auth.
			// $TIX_SESSION_SETTINGS carries your own rules (e.g. which
			// MCP tools a sync may never call).
			wrote = true
			argv, err := session.Command()
			if err != nil {
				return err
			}
			if s := os.Getenv("TIX_SESSION_SETTINGS"); s != "" {
				argv = append(argv, "--settings", s)
			}
			argv = append(argv, "-p", claudePrompt, "--model", model, "--strict-mcp-config",
				"--permission-mode", "dontAsk",
				"--allowedTools", "mcp__"+sc.MCP+",Read,Write,Edit,Glob,Grep,Bash(date:*)",
				"--output-format", "stream-json", "--verbose")
			if err := headlessIn(ctx.Vault, summary, argv); err != nil {
				failed = append(failed, src)
				followups = append(followups, fmt.Sprintf("ticket-sync %s: Claude's run failed; check %s", src, config.TildePath(logFile)))
			}
			if data, err := os.ReadFile(fuFile); err == nil {
				for _, l := range strings.Split(string(data), "\n") {
					if l != "" {
						followups = append(followups, l)
					}
				}
			}
			_ = os.Remove(fuFile)
			_ = os.Remove(planFile)
		}

		// In plan mode tix sync keeps the sync state (the skill keeps it otherwise)
		if js != nil && !contains(failed, src) {
			if err := saveSyncState(stateFile, src, js, ticketsFailed); err != nil {
				warnf("couldn't update %s: %v", stateFile, err)
			}
		}
	}
	if data, err := os.ReadFile(summary); err == nil {
		logf.Write(data)
	}

	noteName := ""
	if len(followups) > 0 {
		n, err := followup.Write(ctx.Vault, "ticket-sync", "ticket-sync", followups, "tickets.base")
		if err != nil {
			warnf("couldn't write the follow-up note: %v", err)
		} else if n != "" {
			rel, _ := filepath.Rel(ctx.Vault, n)
			both("tix sync: follow-ups for you in " + rel)
			noteRel = filepath.ToSlash(rel)
			noteName = filepath.Base(n)
		}
	}

	if len(failed) == 0 && len(ticketsFailed) == 0 {
		data, _ := os.ReadFile(summary)
		body := lastLines(string(data), 8)
		if noteName != "" {
			body += "\nFollow-ups: " + noteName
		}
		notify("normal", "tix sync done", body)
		return nil
	}
	body := "See " + logFile
	if noteName != "" {
		body += " and " + noteName
	}
	title := fmt.Sprintf("tix sync: %d ticket(s) failed", len(ticketsFailed))
	if len(failed) > 0 {
		title = "tix sync: " + strings.Join(failed, " ") + " failed"
	}
	notify("critical", title, body)
	return exitError(1)
}

func headlessIn(dir, log string, argv []string) error {
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		return err
	}
	defer os.Chdir(wd)
	return session.Headless(log, argv)
}

func lastLines(s string, n int) string {
	ls := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.Join(ls, "\n")
}

func notify(urgency, title, body string) {
	u := platform.Normal
	if urgency == "critical" {
		u = platform.Critical
	}
	platform.Notify("tix sync", title, body, u)
}

// knownChildren is the epics' children the last planned run recorded
// (nil: none yet, so this run records them without refreshing every epic).
func knownChildren(stateFile, src string) map[string][]string {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil
	}
	var st struct {
		Sources map[string]struct {
			EpicChildren map[string][]string `json:"epicChildren"`
		} `json:"sources"`
	}
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	return st.Sources[src].EpicChildren
}

func saveSyncState(stateFile, src string, js *jira.Sync, failedTickets []string) error {
	return workspace.Update(stateFile, func(doc map[string]any) error {
		sources, _ := doc["sources"].(map[string]any)
		if sources == nil {
			sources = map[string]any{}
		}
		p := js.Plan
		updated := []string{}
		for _, r := range p.Refresh {
			updated = append(updated, r.ID)
		}
		blocked := []string{}
		for _, b := range p.Blocked {
			blocked = append(blocked, b.ID)
		}
		children := map[string][]string{}
		for k, v := range p.EpicChildren {
			children[k] = v
		}
		for k, v := range js.ChildSigs() {
			children[k] = v
		}
		// An epic that failed keeps no baseline, so the next run refreshes it
		for _, k := range failedTickets {
			delete(children, k)
		}
		sources[src] = map[string]any{
			"lastSync": time.Now().Format(time.RFC3339), "open": p.Counts.Open, "skipped": p.Counts.Skipped,
			"created": p.Create, "updated": updated, "closed": p.Close, "gone": p.Gone, "blocked": blocked,
			"epicChildren": children,
		}
		doc["sources"] = sources
		return nil
	})
}

package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/assets"
	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/container"
	"github.com/zero4573/claude-tickets/internal/fsx"
	"github.com/zero4573/claude-tickets/internal/links"
	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/prompt"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/vaultlock"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

type setupOpts struct {
	defaults, allowOverlap, missing bool
	sections                        []string
}

func vaultInitCmd() *cobra.Command {
	var o setupOpts
	cmd := &cobra.Command{
		Use:   "init [<vault>] [--defaults] [--allow-overlap]",
		Short: "Set a vault up for the ticket workflow",
		Long: `Sets an Obsidian vault up for the ticket workflow. <vault> is a name under
~/Documents/Obsidian (created if it doesn't exist) or a path; without it
you pick one of the existing vaults.

  * folders: raw, tickets, templates, inbox, logs, references,
    knowledge-base, projects/system/{architecture,sequences,features,data,logs}
  * files: AGENTS.md, templates/, tickets.base (dashboard), the task views
    (pending.md, done.md, follow-ups.md), projects/system notes, the
    Atlassian setup reference
  * Obsidian settings: the templates folder, the Bases / Templates /
    Properties core plugins, and the workflow's property types
  * its settings, through ct vault configure: where its workspaces and main
    clones live (.workflow.json, checked for overlaps with other vaults)
    and which ticket sources it uses (tickets/.sources.json). Settings
    already made are kept; change them with ct vault configure.

Existing files are never overwritten (they're listed as kept), so it's
safe to re-run, e.g. to add files a newer version of the workflow ships.
Without a terminal it runs as with --defaults.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeVaults,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !prompt.Interactive() {
				o.defaults = true
			}
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return vaultInit(target, o)
		},
	}
	cmd.Flags().BoolVar(&o.defaults, "defaults", false, "don't prompt: default locations, manual tickets on, Jira configured but disabled")
	cmd.Flags().BoolVar(&o.allowOverlap, "allow-overlap", false, "with --defaults: accept default folders that overlap another vault's")
	return cmd
}

func completeVaults(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return vault.List(), cobra.ShellCompDirectiveNoFileComp
}

var vaultFolders = []string{"raw", "tickets", "templates", "inbox", "logs", "references", "knowledge-base",
	"projects/system/architecture", "projects/system/sequences", "projects/system/features",
	"projects/system/data", "projects/system/logs"}

func vaultInit(target string, o setupOpts) error {
	var v string
	switch {
	case target == "":
		vaults := vault.List()
		if !o.defaults || len(vaults) <= 1 {
			if len(vaults) == 0 {
				return fmt.Errorf("no Obsidian vaults under %s; name one to create it: ct vault init <name>", config.ObsidianRoot())
			}
			name := vaults[0]
			if len(vaults) > 1 {
				var err error
				if name, err = prompt.Pick("vault", vaults); err != nil {
					return err
				}
			}
			v = filepath.Join(config.ObsidianRoot(), name)
		} else {
			return errors.New("several vaults; name one: ct vault init <vault>")
		}
	case strings.Contains(target, "/") || fsx.IsDir(filepath.Join(target, ".obsidian")):
		v, _ = filepath.Abs(config.ExpandHome(target))
	default:
		v = filepath.Join(config.ObsidianRoot(), target)
	}
	if !fsx.IsDir(filepath.Join(v, ".obsidian")) {
		if !o.defaults && !prompt.YesNo(fmt.Sprintf("No vault at %s yet. Create it?", v), "y") {
			return exitError(1)
		}
		if err := os.MkdirAll(filepath.Join(v, ".obsidian"), 0o755); err != nil {
			return err
		}
		fmt.Printf("ct vault init: created vault %s (open it in Obsidian: Open folder as vault)\n", v)
	}
	fmt.Printf("ct vault init: setting up %s\n", v)

	for _, d := range vaultFolders {
		if err := os.MkdirAll(filepath.Join(v, d), 0o755); err != nil {
			return err
		}
	}

	// Files, never overwritten
	var added, kept []string
	err := fs.WalkDir(assets.Scaffold, "vault-scaffold", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, "vault-scaffold/")
		if rel == "obsidian-types.json" {
			return nil
		}
		dest := filepath.Join(v, filepath.FromSlash(rel))
		if _, err := os.Lstat(dest); err == nil {
			kept = append(kept, rel)
			return nil
		}
		data, err := assets.Scaffold.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		added = append(added, rel)
		return os.WriteFile(dest, data, 0o644)
	})
	if err != nil {
		return err
	}
	fmt.Printf("ct vault init: added %d file(s), kept %d existing\n", len(added), len(kept))
	for _, a := range added {
		fmt.Printf("  + %s\n", a)
	}

	if err := obsidianSettings(filepath.Join(v, ".obsidian")); err != nil {
		return err
	}

	o.missing = true
	if err := vaultConfigure(v, o); err != nil {
		return err
	}

	if !fsx.IsDir(filepath.Join(v, ".obsidian", "plugins", "obsidian-tasks-plugin")) {
		warnf("the Tasks community plugin isn't installed here: add \"%s\" to programs.claude-tickets.obsidian.vaults (home-manager), or install it from Obsidian", filepath.Base(v))
	}
	fmt.Println("ct vault init: done. Next (on the default vault; ct vault default to switch): ct sync, ct new, ct start <ID>")
	return nil
}

// updateObject changes a JSON object file in place (missing or empty: an
// empty object). Files that aren't an object are left alone, with a warning.
func updateObject(file string, fn func(m map[string]any)) error {
	m := map[string]any{}
	if data, err := os.ReadFile(file); err == nil && strings.TrimSpace(string(data)) != "" {
		v, err := config.ReadJSON(file)
		if err != nil {
			return err
		}
		var ok bool
		if m, ok = v.(map[string]any); !ok {
			warnf("%s isn't a JSON object; left as it is", file)
			return nil
		}
	}
	fn(m)
	return config.WriteJSON(file, m)
}

func obsidianSettings(obs string) error {
	// The core Templates plugin reads templates/; keep a folder the user set
	folder := ""
	if err := updateObject(filepath.Join(obs, "templates.json"), func(m map[string]any) {
		if f, _ := m["folder"].(string); f == "" {
			m["folder"] = "templates"
		}
		folder, _ = m["folder"].(string)
	}); err != nil {
		return err
	}
	if folder != "templates" {
		warnf("the Templates plugin uses folder '%s', not templates/", folder)
	}
	if err := updateObject(filepath.Join(obs, "core-plugins.json"), func(m map[string]any) {
		for _, p := range []string{"templates", "bases", "properties"} {
			m[p] = true
		}
	}); err != nil {
		return err
	}
	// Property types: add the workflow's, keep any the vault already has
	data, err := assets.Scaffold.ReadFile("vault-scaffold/obsidian-types.json")
	if err != nil {
		return err
	}
	var types map[string]any
	if err := jsonUnmarshal(data, &types); err != nil {
		return err
	}
	return updateObject(filepath.Join(obs, "types.json"), func(m map[string]any) {
		have, _ := m["types"].(map[string]any)
		merged := map[string]any{}
		for k, t := range types {
			merged[k] = t
		}
		for k, t := range have {
			merged[k] = t
		}
		m["types"] = merged
	})
}

var allSections = []string{"locations", "sources", "runtime"}

func vaultConfigureCmd() *cobra.Command {
	var o setupOpts
	cmd := &cobra.Command{
		Use:   "configure [<vault>] [--section <name>]... [--missing] [--defaults] [--allow-overlap]",
		Short: "A vault's settings: locations, ticket sources, container runtime",
		Long: `Configures a vault for the ticket workflow, one section at a time. Every
prompt shows the current value as its default, so it changes an existing
vault as easily as it sets up a new one (ct vault init runs it). <vault> is
a name under ~/Documents/Obsidian or a path; default: the current vault.

Sections (all of them unless --section is given):
  locations  .workflow.json: where the vault's tools work
               workRoot      ticket and kb workspaces (~/Projects/work-<vault>)
               projectsRoot  main clones (~/Projects/repo-<vault>)
             (The ticket windows' tmux session is always tickets-<vault>.)
             A folder that is the same as, inside, or around another
             vault's folder (or this vault's other one) is shown with what
             it overlaps, and kept only when you type yes. Vaults sharing
             projectsRoot share their main clones, which is fine when meant;
             sharing workRoot mixes their workspaces (a MAN-1 exists in
             each). Moving workRoot doesn't move the workspaces already in
             the old one.
  sources    tickets/.sources.json: the ticket sources (manual, Jira). The
             old file is kept as tickets/.sources.json.bak; settings the
             prompts don't cover (other sources, model, textFields) are
             kept.
  runtime    podman or docker for the code graph, host-wide
             (` + config.TildePath(config.File()) + `; default: detected,
             CLAUDE_TICKETS_CONTAINER overrides it)

Without a terminal it runs as with --defaults.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeVaults,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, s := range o.sections {
				if !contains(allSections, s) {
					return fmt.Errorf("no section '%s' (sections: %s)", s, strings.Join(allSections, " "))
				}
			}
			if !prompt.Interactive() {
				o.defaults = true
			}
			var v string
			var err error
			if len(args) == 1 {
				v, err = namedVault(args[0])
			} else {
				v, err = vault.Current()
			}
			if err != nil {
				return err
			}
			return vaultConfigure(v, o)
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(&o.sections, "section", nil, "configure only this section: locations, sources or runtime (repeatable)")
	f.BoolVar(&o.missing, "missing", false, "only the sections not configured yet")
	f.BoolVar(&o.defaults, "defaults", false, "don't prompt: keep sections already configured, write the defaults for the others")
	f.BoolVar(&o.allowOverlap, "allow-overlap", false, "with --defaults: accept overlapping folders instead of failing")
	return cmd
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// namedVault is a vault given by path, or by name under the Obsidian root.
func namedVault(name string) (string, error) {
	if fsx.IsDir(filepath.Join(name, ".obsidian")) {
		return filepath.Abs(name)
	}
	p := filepath.Join(config.ObsidianRoot(), name)
	if !fsx.IsDir(filepath.Join(p, ".obsidian")) {
		return "", fmt.Errorf("no vault named '%s' under %s", name, config.ObsidianRoot())
	}
	return p, nil
}

func vaultConfigure(v string, o setupOpts) error {
	sections := o.sections
	if len(sections) == 0 {
		sections = allSections
	}
	fmt.Printf("ct vault configure: %s\n", v)
	for _, s := range sections {
		var file string
		var configured bool
		switch s {
		case "locations":
			file = filepath.Join(v, ".workflow.json")
		case "sources":
			file = filepath.Join(v, "tickets", ".sources.json")
		case "runtime":
			file = config.File()
			configured = config.Get("container") != ""
		}
		if s != "runtime" {
			_, err := os.Stat(file)
			configured = err == nil
		}
		if configured && (o.missing || o.defaults) {
			fmt.Printf("ct vault configure: kept %s (ct vault configure --section %s to change it)\n", config.TildePath(file), s)
			continue
		}
		var err error
		switch s {
		case "locations":
			err = configureLocations(v, file, o)
		case "sources":
			err = configureSources(file, o)
		case "runtime":
			err = configureRuntime(o)
		}
		if err != nil {
			return err
		}
	}

	// The vault the commands act on (the saved default, not a session's)
	if !o.defaults {
		saved := ""
		if s := vault.SavedDefault(); s != "" {
			saved, _ = vault.Resolve(s)
		}
		if !vault.PathsSame(saved, v) {
			fmt.Println()
			if prompt.YesNo(fmt.Sprintf("Make %s the default vault, the one the ticket commands act on?", filepath.Base(v)), "y") {
				s, err := vault.SetDefault(v)
				if err != nil {
					return err
				}
				fmt.Printf("ct vault configure: default vault is now %s\n", s)
			}
		}
	}
	return nil
}

// workflowFile is .workflow.json, with its explanation first.
type workflowFile struct {
	Comment      string `json:"_comment"`
	WorkRoot     string `json:"workRoot"`
	ProjectsRoot string `json:"projectsRoot"`
}

func configureLocations(v, file string, o setupOpts) error {
	cur := vault.LocationsOf(v)
	w, p := cur.WorkRoot, cur.ProjectsRoot
	oldW := w
	if !o.defaults {
		fmt.Printf("\nLocations (%s); press Enter to keep the value shown.\n", config.TildePath(file))
	}
	for {
		if !o.defaults {
			w = config.ExpandHome(prompt.Ask("  workRoot: ticket and kb workspaces", config.TildePath(w)))
			p = config.ExpandHome(prompt.Ask("  projectsRoot: main clones", config.TildePath(p)))
		}
		var overlaps []string
		if vault.PathsOverlap(w, p) {
			overlaps = append(overlaps, fmt.Sprintf("workRoot %s and projectsRoot %s overlap each other", config.TildePath(w), config.TildePath(p)))
		}
		for _, other := range vault.List() {
			ov := filepath.Join(config.ObsidianRoot(), other)
			if vault.PathsSame(ov, v) {
				continue
			}
			l := vault.LocationsOf(ov)
			if vault.PathsOverlap(w, l.WorkRoot) {
				overlaps = append(overlaps, fmt.Sprintf("workRoot %s overlaps %s's workRoot %s: the two vaults' workspaces mix, and a MAN-1 exists in each", config.TildePath(w), other, config.TildePath(l.WorkRoot)))
			}
			if vault.PathsOverlap(w, l.ProjectsRoot) {
				overlaps = append(overlaps, fmt.Sprintf("workRoot %s overlaps %s's projectsRoot %s", config.TildePath(w), other, config.TildePath(l.ProjectsRoot)))
			}
			if vault.PathsOverlap(p, l.WorkRoot) {
				overlaps = append(overlaps, fmt.Sprintf("projectsRoot %s overlaps %s's workRoot %s", config.TildePath(p), other, config.TildePath(l.WorkRoot)))
			}
			if vault.PathsOverlap(p, l.ProjectsRoot) {
				overlaps = append(overlaps, fmt.Sprintf("projectsRoot %s overlaps %s's projectsRoot %s: the vaults share main clones", config.TildePath(p), other, config.TildePath(l.ProjectsRoot)))
			}
		}
		if len(overlaps) > 0 {
			for _, ov := range overlaps {
				fmt.Fprintf(os.Stderr, "  ! %s\n", ov)
			}
			if o.defaults {
				if !o.allowOverlap {
					return errors.New("overlapping folders (--allow-overlap to accept them, or run ct vault configure in a terminal)")
				}
			} else if !prompt.ConfirmYes("  Keep these overlapping folders?") {
				fmt.Println("  Then pick other folders.")
				continue
			}
		}
		// Workspaces stay where they are when workRoot moves
		if _, err := os.Stat(file); err == nil && !vault.PathsSame(w, oldW) {
			if old := workspace.List(oldW, true); len(old) > 0 {
				fmt.Fprintf(os.Stderr, "  ! these workspaces stay in the old workRoot %s, and the ticket tools won't see them there:\n", config.TildePath(oldW))
				for _, d := range old {
					fmt.Fprintf(os.Stderr, "      %s\n", d)
				}
				if o.defaults || !prompt.ConfirmYes("  Move workRoot anyway (move them yourself after)?") {
					continue
				}
			}
		}
		break
	}
	if err := config.WriteJSON(file, workflowFile{
		Comment:      "Where the ticket tools of this vault work: workRoot holds the ticket and kb workspaces, projectsRoot the main clones (vaults may share it). The ticket windows run in the tmux session tickets-<vault>. Change them with ct vault configure --section locations, which checks for overlaps with other vaults.",
		WorkRoot:     config.TildePath(w),
		ProjectsRoot: config.TildePath(p),
	}); err != nil {
		return err
	}
	fmt.Printf("ct vault configure: wrote .workflow.json (workspaces in %s, main clones in %s, tmux session tickets-%s)\n",
		config.TildePath(w), config.TildePath(p), filepath.Base(v))
	return nil
}

// sourcesTemplate is a new tickets/.sources.json: every known source.
const sourcesTemplate = `{
  "_comment": "Ticket sources for this vault. ct sync pulls every enabled source whose sync is not false, through the MCP server named by mcp (its URL comes from the MCP config in CLAUDE_TICKETS_MCP_CONFIG; the headless Claude step uses the servers your claude command provides). A source also needs an adapter: assets/plugin/skills/ticket-sync/sources/<name>.md in claude-tickets. Local ticket IDs are the source key when idPrefix is empty, otherwise <idPrefix>-<native id>. manual tickets are written in Obsidian (ct new, templates/ticket-manual.md) and never synced. The jira source finds its Jira site from the token; set \"site\": \"<name>.atlassian.net\" only if the token can reach several. Change the manual and jira settings with ct vault configure --section sources.",
  "sources": {
    "jira": {},
    "manual": {},
    "servicenow": {"enabled": false, "mcp": "servicenow", "idPrefix": "SNOW",
                   "query": "assigned_to=javascript:gs.getUserID()^active=true"},
    "zendesk": {"enabled": false, "mcp": "zendesk", "idPrefix": "ZD",
                "query": "assignee:me status<solved"}
  }
}`

// mcpServers is the server names in $CLAUDE_TICKETS_MCP_CONFIG, sorted.
func mcpServers() []string {
	f := os.Getenv("CLAUDE_TICKETS_MCP_CONFIG")
	if f == "" {
		return nil
	}
	v, err := config.ReadJSON(f)
	if err != nil {
		return nil
	}
	servers, _ := asMap(v)["mcpServers"].(map[string]any)
	var names []string
	for k := range servers {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

func configureSources(file string, o setupOpts) error {
	doc := map[string]any{}
	existing := false
	if _, err := os.Stat(file); err == nil {
		v, err := config.ReadJSON(file)
		if err != nil {
			return err
		}
		doc, existing = asMap(v), true
	}
	sources := asMap(doc["sources"])
	jiraCur, manualCur := asMap(sources["jira"]), asMap(sources["manual"])
	str := func(m map[string]any, k, def string) string {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
		return def
	}
	boolean := func(m map[string]any, k string, def bool) bool {
		if b, ok := m[k].(bool); ok {
			return b
		}
		return def
	}
	manual := boolean(manualCur, "enabled", true)
	manualPrefix := str(manualCur, "idPrefix", "MAN")
	jira := boolean(jiraCur, "enabled", false)
	jiraMCP := str(jiraCur, "mcp", "atlassian")
	jiraPrefix := str(jiraCur, "idPrefix", "")
	jiraQuery := str(jiraCur, "query", "assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC")
	jiraSite := str(jiraCur, "site", "")

	yn := func(b bool) string {
		if b {
			return "y"
		}
		return "n"
	}
	if !o.defaults {
		fmt.Printf("\nTicket sources (%s); press Enter to keep the value shown.\n", config.TildePath(file))
		manual = prompt.YesNo("Manual tickets, written in Obsidian (ct new)?", yn(manual))
		if manual {
			for {
				manualPrefix = prompt.Ask("  ID prefix for manual tickets (<prefix>-<n>)", manualPrefix)
				if note.ValidKey(manualPrefix + "-1") {
					break
				}
				fmt.Println("  An ID prefix is upper-case letters/digits starting with a letter, e.g. MAN")
			}
		}
		registered := mcpServers()
		jiraDefault := yn(jira)
		if !existing {
			jiraDefault = yn(contains(registered, "atlassian"))
		}
		jira = prompt.YesNo("Pull Jira tickets assigned to you (ct sync)?", jiraDefault)
		if jira {
			if len(registered) > 0 {
				fmt.Printf("  MCP servers in %s: %s\n", os.Getenv("CLAUDE_TICKETS_MCP_CONFIG"), strings.Join(registered, " "))
			}
			jiraMCP = prompt.Ask("  MCP server for Jira (its name in CLAUDE_TICKETS_MCP_CONFIG)", jiraMCP)
			if !contains(registered, jiraMCP) {
				cfg := os.Getenv("CLAUDE_TICKETS_MCP_CONFIG")
				if cfg == "" {
					cfg = "unset"
				}
				warnf("'%s' isn't in CLAUDE_TICKETS_MCP_CONFIG (%s) yet; see references/atlassian-rovo-mcp-setup.md", jiraMCP, cfg)
			}
			jiraQuery = prompt.Ask("  JQL for your open tickets", jiraQuery)
			site := jiraSite
			if site == "" {
				site = "-"
			}
			if jiraSite = prompt.Ask("  Jira site, only if the token reaches several (- = find it)", site); jiraSite == "-" {
				jiraSite = ""
			}
		}
	}

	// A new file starts from the template (every known source, disabled);
	// an existing one keeps what the prompts don't cover
	if !existing {
		if err := jsonUnmarshal([]byte(sourcesTemplate), &doc); err != nil {
			return err
		}
	} else {
		data, _ := os.ReadFile(file)
		if err := os.WriteFile(file+".bak", data, 0o644); err != nil {
			return err
		}
	}
	sources = asMap(doc["sources"])
	j := asMap(sources["jira"])
	j["enabled"], j["mcp"], j["idPrefix"], j["query"] = jira, jiraMCP, jiraPrefix, jiraQuery
	if jiraSite == "" {
		delete(j, "site")
	} else {
		j["site"] = jiraSite
	}
	m := asMap(sources["manual"])
	m["enabled"], m["sync"], m["idPrefix"] = manual, false, manualPrefix
	sources["jira"], sources["manual"] = j, m
	doc["sources"] = sources
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	if err := config.WriteJSON(file, doc); err != nil {
		return err
	}
	fmt.Printf("ct vault configure: wrote tickets/.sources.json (jira: %t, manual: %t)\n", jira, manual)
	return nil
}

func configureRuntime(o setupOpts) error {
	current := config.Get("container")
	detected, _ := container.Detect()
	var rt string
	if o.defaults {
		rt = current
		if rt == "" {
			rt = detected
		}
	} else {
		fmt.Printf("\nContainer runtime for the code graph (graphify), host-wide (%s).\n", config.TildePath(config.File()))
		if detected != "" {
			fmt.Printf("  Detected: %s\n", detected)
		}
		def := current
		if def == "" {
			def = detected
		}
		if def == "" {
			def = "podman"
		}
		for {
			if rt = prompt.Ask("  podman or docker", def); rt == "podman" || rt == "docker" {
				break
			}
			fmt.Println("  It's podman or docker.")
		}
		if _, err := exec.LookPath(rt); err != nil {
			warnf("%s isn't installed yet", rt)
		}
	}
	if rt == "" {
		warnf("no container runtime found (podman or docker); the code graph needs one")
		return nil
	}
	if err := config.Set("container", rt); err != nil {
		return err
	}
	fmt.Printf("ct vault configure: container runtime is %s (CLAUDE_TICKETS_CONTAINER overrides it)\n", rt)
	return nil
}

func vaultLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock acquire|release|status|run ...",
		Short: "The lock /tickets:save holds while writing shared notes",
		Long: `An exclusive lock on a vault's shared notes (projects/, knowledge-base/,
references/), so concurrent ticket sessions running /tickets:save take
turns. <owner> identifies the holder, e.g. the ticket key.

The lock is the directory <vault>/.vault.lock.d (mkdir is atomic), holding
an owner file; stale takeovers serialize on <vault>/.vault.lock.d.guard.
Dot-files are hidden from Obsidian.`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "acquire <vault> <owner>",
		Short: "Wait (up to 10 minutes) for the lock, then take it; a lock idle for 30 minutes is taken over",
		Args:  cobra.ExactArgs(2),
		RunE:  func(cmd *cobra.Command, args []string) error { return vaultlock.Acquire(args[0], args[1]) },
	}, &cobra.Command{
		Use:   "release <vault> <owner>",
		Short: "Free the lock if <owner> holds it",
		Args:  cobra.ExactArgs(2),
		RunE:  func(cmd *cobra.Command, args []string) error { return vaultlock.Release(args[0], args[1]) },
	}, &cobra.Command{
		Use:   "status <vault>",
		Short: "Who holds the lock",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := vaultlock.Status(args[0])
			if err == nil {
				fmt.Println(s)
			}
			return err
		},
	}, &cobra.Command{
		Use:   "run <vault> <owner> -- <command> [args...]",
		Short: "Acquire, run the command, release",
		Args: func(cmd *cobra.Command, args []string) error {
			if cmd.ArgsLenAtDash() != 2 || len(args) < 3 {
				return errors.New("usage: ct vault lock run <vault> <owner> -- <command> [args...]")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := vaultlock.Acquire(args[0], args[1]); err != nil {
				return err
			}
			c := exec.Command(args[2], args[3:]...)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			runErr := c.Run()
			if err := vaultlock.Release(args[0], args[1]); err != nil {
				warnf("%v", err)
			}
			return passExit(runErr)
		},
	})
	return cmd
}

func vaultLinksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "links check|move ...",
		Short: "Check wikilinks; move a note without breaking links to it",
		Long: `Checks and preserves Obsidian wikilinks when notes move. Resolution follows
Obsidian: a link target without "/" matches by file name (case-insensitive,
".md" implied for notes); with "/" it matches the end of the vault-relative
path. Links inside code blocks and inline code are ignored. .obsidian/,
.trash/ and other dot-folders are skipped.`,
	}
	vaultArg := func(p string) (string, error) {
		v, err := filepath.Abs(p)
		if err != nil {
			return "", err
		}
		if r, err := filepath.EvalSymlinks(v); err == nil {
			v = r
		}
		if !fsx.IsDir(filepath.Join(v, ".obsidian")) {
			return "", exitWith(2, fmt.Errorf("not an Obsidian vault: %s", v))
		}
		return v, nil
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "check <vault> [file...]",
		Short: "Report links that resolve to no note, or to several; exit 1 if any",
		Long: `Reports wikilinks and embeds ([[x]], ![[x]], [[x#heading]], [[x|alias]])
that resolve to no note, or to more than one note (ambiguous by bare name).
Without files, checks the whole vault except templates/. Exits 1 if
anything is broken. A file is a path, or relative to the vault.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := vaultArg(args[0])
			if err != nil {
				return err
			}
			var files []string
			for _, a := range args[1:] {
				f := a
				if _, err := os.Stat(a); !filepath.IsAbs(a) && err != nil {
					f = filepath.Join(v, a)
				}
				if abs, err := filepath.Abs(f); err == nil {
					f = abs
				}
				if r, err := filepath.EvalSymlinks(f); err == nil {
					f = r
				}
				files = append(files, f)
			}
			if links.Check(v, files, os.Stdout) > 0 {
				return exitError(1)
			}
			return nil
		},
	}, &cobra.Command{
		Use:   "move <vault> <src> <dst>",
		Short: "Move a note inside the vault, keeping every link to it working; prints the new path",
		Long: `Moves a note (or attachment) inside the vault, keeping every link to it
working: refuses if the destination's name is already taken by another file
(bare-name links would become ambiguous), then rewrites any path-qualified
links ([[old/path/note]]) across the vault to the new path. Bare-name links
([[note]]) only change when the name does (a rename), and then follow it.
<dst> may be a folder. Prints the new path.`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := vaultArg(args[0])
			if err != nil {
				return err
			}
			rel, n, err := links.Move(v, args[1], args[2])
			if err != nil {
				return exitWith(2, err)
			}
			fmt.Println(rel)
			if n > 0 {
				fmt.Fprintf(os.Stderr, "ct vault links: rewrote %d path-qualified link(s)\n", n)
			}
			return nil
		},
	})
	return cmd
}

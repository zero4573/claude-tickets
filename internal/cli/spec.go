package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zero4573/tix/internal/config"
	"github.com/zero4573/tix/internal/fsx"
	"github.com/zero4573/tix/internal/gitx"
	"github.com/zero4573/tix/internal/harness"
	"github.com/zero4573/tix/internal/modelcache"
	"github.com/zero4573/tix/internal/note"
	"github.com/zero4573/tix/internal/repo"
	"github.com/zero4573/tix/internal/session"
	"github.com/zero4573/tix/internal/spec"
	"github.com/zero4573/tix/internal/vault"
	"github.com/zero4573/tix/internal/workspace"
)

func specCmd() *cobra.Command {
	var cont, print, noFetch bool
	var agent string
	cmd := &cobra.Command{
		Use:   `spec [--continue] [--agent claude|kiro] [--print] [--no-fetch] ["<question>"]`,
		Short: "Ask about the projects the vault describes, without a ticket",
		Long: `Ask about the projects the vault describes (the current vault, tix vault
default): repos, how they work together, architecture, data flows, past
tickets, without creating a ticket.
It runs a Claude session (the spec skill) with:
  * the vault, read-write, as the knowledge base
  * every repo in its projectsRoot, read-only, freshly fetched on the host; the
    session explores branches in its own clones (tix spec repo), where it may
    build and test, but never commits or pushes
  * tix new, to turn issues it finds into manual tickets
  * the MCP servers your claude command provides

Unknowns get investigated. What's learned is drafted into the vault's
inbox/, and /tix:save in the session promotes it into projects/,
knowledge-base/ and references/.

--print answers one question and exits (claude -p), no session; progress
is shown live and logged to ` + logPath("spec-<vault>") + `.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			question := strings.Join(args, " ")
			if print && question == "" {
				return errors.New("--print needs a question")
			}
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			return specSession(ctx, question, cont, print, !noFetch, agent)
		},
	}
	cmd.Flags().BoolVarP(&cont, "continue", "c", false, "continue the last spec conversation for this vault")
	cmd.Flags().BoolVarP(&print, "print", "p", false, "answer one question and exit")
	cmd.Flags().BoolVar(&noFetch, "no-fetch", false, "skip fetching every main clone from its remote first")
	cmd.Flags().StringVar(&agent, "agent", "", agentHelp)
	cmd.AddCommand(specRepoCmd(), specLocationCmd(), specCheckCmd(), specDiffCmd(), specStartCmd(), specPromoteCmd(),
		specPinCmd(), specLogCmd(), specShowCmd())
	return cmd
}

func specSession(ctx vault.Context, question string, cont, print, fetch bool, agent string) error {
	// A session may have no git credentials (e.g. sandboxed), so bring every
	// main clone's remote branches up to date here; tix spec repo fetch passes
	// them on to the session
	if fetch {
		if err := wsFetch(ctx, nil); err != nil {
			return err
		}
	}
	name := filepath.Base(ctx.Vault)
	// A workspace like a ticket's, but with no repos of its own (hidden, so
	// tix status and tix ws ls skip it)
	dir := filepath.Join(ctx.WorkRoot, ".spec-"+name)
	// The workspace was once called .kb-<vault>: keep its clones
	if moved, err := workspace.Migrate(filepath.Join(ctx.WorkRoot, ".kb-"+name), dir); err != nil {
		return err
	} else if moved {
		warnf("renamed %s to %s", ".kb-"+name, filepath.Base(dir))
	}
	if err := workspace.Ensure(dir, "SPEC", ctx.Vault); err != nil {
		return err
	}
	if err := workspace.Update(workspace.File(dir), func(doc map[string]any) error {
		doc["id"] = "SPEC"
		return nil
	}); err != nil {
		return err
	}
	h, err := harness.Select(agent, vault.AgentOf(ctx.Vault))
	if err != nil {
		return err
	}
	if prev := workspace.Agent(dir); cont && prev != h.Name() {
		warnf("the last spec session ran %s, not %s, so it can't be continued: starting a new one", prev, h.Name())
		cont = false
	}
	if err := workspace.SetAgent(dir, h.Name()); err != nil {
		warnf("couldn't record the agent in %s: %v", workspace.File(dir), err)
	}
	// Identifies this session's inbox/ drafts for /tix:save
	sessionID := "spec-" + name + "-" + time.Now().Format("20060102-1504")
	// The exploration clones (in this directory) are the session's own, so it
	// may edit, build and test in them. The main clones stay untouched, and
	// nothing is committed or pushed.
	writeToolVersions(ctx, dir)
	s := harness.Session{
		Dir:          dir,
		Dirs:         []string{ctx.Vault},
		ReposRoot:    ctx.ProjectsRoot,
		ReadOnly:     []string{modelcache.Dir()},
		DenyShell:    []string{"git commit", "git push"},
		Instructions: specClaudeMD(ctx, dir, sessionID),
		Settings:     true,
		Skill:        "spec",
		Args:         question,
		Continue:     cont,
		Print:        print,
	}
	if question == "" {
		s.Skill = ""
	}
	if err := h.Prepare(s); err != nil {
		return err
	}
	argv, err := h.Argv(s)
	if err != nil {
		return err
	}
	setEnv(h.Env(s))
	if err := os.Chdir(dir); err != nil {
		return err
	}
	// kiro's one-shot answer goes straight to this terminal (tix reads
	// Claude's stream-json progress only)
	if !print || h.Name() != harness.Claude {
		path, err := exec.LookPath(argv[0])
		if err != nil {
			return err
		}
		// So tix ws gc sees the session running while its process lives
		if err := workspace.RecordSession(dir, "spec"); err != nil {
			warnf("couldn't record the session in %s: %v", workspace.SessionsFile(dir), err)
		}
		return passExit(execReplaceOrRun(path, argv))
	}
	// One-shot answer: progress as it happens, also logged
	log := config.StateLog("spec-" + name)
	if f, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintf(f, "=== spec %s vault=%s: %s\n", time.Now().Format(time.RFC3339), ctx.Vault, question)
		f.Close()
	}
	return passExit(session.Headless(log, argv))
}

// setEnv puts a harness's session variables in tix's own environment, for
// the sessions it execs rather than launches (tix spec, tix claude).
func setEnv(env map[string]string) {
	for k, v := range env {
		os.Setenv(k, v)
	}
}

func specClaudeMD(ctx vault.Context, dir, sessionID string) string {
	return fmt.Sprintf("# Spec session\n\n"+
		"Written by tix spec; regenerated on every start, so don't edit it.\n\n"+
		"- Vault (knowledge base, read-write): `%[1]s`. Read its `AGENTS.md`.\n"+
		"- Repos: main clones at `%[2]s/<provider>/<owner>/<repo>`, read-only,\n"+
		"  fetched from their remotes when this session started. `tix ws repos`\n"+
		"  lists every repo with its slug.\n"+
		"- **Exploring branches:** `tix spec repo checkout <repo> [<branch>|<tag>|<commit>]`\n"+
		"  gives you an exploration clone at `%[3]s/<slug>` on that ref.\n"+
		"  `tix spec repo fetch` refreshes remote branches, `tix spec repo ls` shows\n"+
		"  what's checked out, `tix spec repo reset` drops clones.\n"+
		"- **Experiments are fine, changes aren't kept:** in your exploration clones\n"+
		"  you may edit, build, run tests and add debug output to check how something\n"+
		"  behaves. Those edits stay in this session: never commit or push (both\n"+
		"  denied), and `tix spec repo checkout --force` discards them. The main clones in\n"+
		"  `%[2]s` are never edited.\n"+
		"- **Issues become tickets:** when you find something that should be\n"+
		"  investigated or fixed, create a manual ticket with\n"+
		"  `tix new \"<summary>\"` and fill it in (see the `spec`\n"+
		"  skill), so the work goes through the ticket workflow.\n"+
		"- `tix vault groups %[1]s` shows which projects work together (groups,\n"+
		"  `depends-on`) and which are unrelated.\n"+
		"- Follow the `spec` skill: answer from the vault, then the code. Investigate\n"+
		"  unknowns, draft what you learn into `%[1]s/inbox/`, and run\n"+
		"  `/tix:save` before the session ends to promote the drafts.\n"+
		"- Session id (put it in the `session:` field of your inbox/ drafts):\n"+
		"  `%[4]s`\n",
		ctx.Vault, ctx.ProjectsRoot, dir, sessionID)
}

// specRepo is an exploration clone of a spec session.
type specRepo struct {
	ctx               vault.Context
	dir, ws           string // the spec workspace and its workspace.json
	clone, slug, main string
	path              string
}

// specWorkspace finds the spec workspace: $SPEC_DIR, else the nearest folder up
// from here whose workspace.json is the spec's (so it also works from inside
// an exploration clone).
func specWorkspace() (string, error) {
	start := os.Getenv("SPEC_DIR")
	if start == "" {
		start, _ = os.Getwd()
	}
	for d := start; ; d = filepath.Dir(d) {
		if info, err := workspace.Read(d); err == nil && info.ID == "SPEC" {
			return d, nil
		}
		if d == filepath.Dir(d) {
			return "", fmt.Errorf("not in a spec session (no spec workspace.json above %s)", start)
		}
	}
}

func specRepoOf(ctx vault.Context, dir, arg string) (specRepo, error) {
	clone, ok := repo.Resolve(ctx.ProjectsRoot, arg)
	if !ok {
		return specRepo{}, fmt.Errorf("no main clone '%s' (see tix ws repos)", arg)
	}
	slug := repo.Slug(clone)
	return specRepo{ctx, dir, workspace.File(dir), clone, slug, filepath.Join(ctx.ProjectsRoot, clone), filepath.Join(dir, slug)}, nil
}

// refresh copies remote branches and tags from the main clone, then from
// the remote itself where it can be reached.
func (r specRepo) refresh() {
	_ = gitx.RefreshSharedClone(r.main, r.path)
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	git := gitx.CommandContext(c, "-C", r.path, "fetch", "--quiet", "--prune", "origin")
	// Never prompt: no credentials, no host-key questions, just fail fast
	git.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=10")
	if git.Run() != nil {
		fmt.Fprintf(os.Stderr, "tix spec repo: %s: can't reach origin from here; using the main clone's fetch (run tix ws fetch %s on the host for newer)\n", r.slug, r.slug)
	}
}

func (r specRepo) lastCommit() string {
	s, _ := gitx.Out(r.path, "log", "-1", "--format=%h %cs %s")
	return s
}

func (r specRepo) exists() bool { return fsx.IsDir(filepath.Join(r.path, ".git")) }

func exploreSlugs(ws string) []string {
	info, _ := workspace.Read(filepath.Dir(ws))
	var out []string
	for _, r := range info.Repos {
		if r.Mode == "explore" {
			out = append(out, r.Slug)
		}
	}
	return out
}

func specRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Exploration clones of a spec session (inside one)",
		Long: `Exploration clones for a spec session: look at any branch or commit of a repo
without touching its main clone (read-only here). Each is a shared clone (it
borrows the main clone's objects) at <workRoot>/.spec-<vault>/<slug>, registered in the
session's workspace.json. A <repo> is a slug or <provider>/<owner>/<repo>.

The clones are the session's own: edit, build and test in them as needed to
check how things behave, but never commit or push (both denied). Edits stay
in the session; checkout --force or reset throws them away.`,
	}
	with := func(fn func(ctx vault.Context, dir string, args []string) error) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, args []string) error {
			dir, err := specWorkspace()
			if err != nil {
				return err
			}
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			return fn(ctx, dir, args)
		}
	}
	var force, all bool
	checkout := &cobra.Command{
		Use:   "checkout [--force] <repo> [<branch>|<tag>|<commit>]",
		Short: "Check out a ref in the repo's exploration clone (created if needed)",
		Long: `Checks out a branch (origin's, as a local branch), a tag or a commit,
creating the exploration clone if needed. Default: the remote's default
branch. Refuses while the clone has local edits; --force discards them.`,
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: completeRepos,
		RunE: with(func(ctx vault.Context, dir string, args []string) error {
			r, err := specRepoOf(ctx, dir, args[0])
			if err != nil {
				return err
			}
			ref := ""
			if len(args) == 2 {
				ref = args[1]
			}
			return r.checkout(ref, force)
		}),
	}
	checkout.Flags().BoolVar(&force, "force", false, "discard local edits in the clone")
	fetch := &cobra.Command{
		Use:   "fetch [<repo>...]",
		Short: "Refresh remote branches and tags (default: every exploration clone)",
		Long: `Refreshes remote branches and tags: from the main clone (as fetched on the
host), then from the remote itself where it can be reached. A clean
checked-out branch is fast-forwarded. Default: every exploration clone.`,
		ValidArgsFunction: completeRepos,
		RunE: with(func(ctx vault.Context, dir string, args []string) error {
			if len(args) == 0 {
				args = exploreSlugs(workspace.File(dir))
			}
			for _, a := range args {
				r, err := specRepoOf(ctx, dir, a)
				if err != nil {
					return err
				}
				if !r.exists() {
					warnf("%s has no exploration clone (tix spec repo checkout %s)", r.slug, r.slug)
					continue
				}
				r.refresh()
				if up, err := gitx.Out(r.path, "rev-parse", "--abbrev-ref", "@{u}"); err == nil && !gitx.Dirty(r.path) {
					if gitx.Timeout(time.Minute, r.path, "merge", "--quiet", "--ff-only", up) != nil {
						warnf("%s: can't fast-forward to %s", r.slug, up)
					}
				}
				fmt.Printf("tix spec repo: %s fetched (%s)\n", r.slug, r.lastCommit())
			}
			return nil
		}),
	}
	ls := &cobra.Command{
		Use:   "ls",
		Short: "Exploration clones: what's checked out, how far behind the remote",
		Args:  cobra.NoArgs,
		RunE: with(func(ctx vault.Context, dir string, args []string) error {
			info, _ := workspace.Read(dir)
			w := table()
			fmt.Fprintln(w, "REPO\tAT\tBEHIND\tCOMMIT")
			for _, r := range info.Repos {
				if r.Mode != "explore" {
					continue
				}
				behind := "-"
				if gitx.HasRef(r.Path, "@{u}") {
					behind, _ = gitx.Out(r.Path, "rev-list", "--count", "HEAD..@{u}")
				}
				commit, _ := gitx.Out(r.Path, "log", "-1", "--format=%h %cs %s")
				if rs := []rune(commit); len(rs) > 60 {
					commit = string(rs[:60])
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Slug, r.Branch, behind, commit)
			}
			return w.Flush()
		}),
	}
	reset := &cobra.Command{
		Use:               "reset <repo> | --all",
		Short:             "Remove exploration clones",
		ValidArgsFunction: completeRepos,
		RunE: with(func(ctx vault.Context, dir string, args []string) error {
			ws := workspace.File(dir)
			if all {
				args = exploreSlugs(ws)
			} else if len(args) != 1 {
				return errors.New("reset: name a <repo>, or --all")
			}
			for _, a := range args {
				r, err := specRepoOf(ctx, dir, a)
				if err != nil {
					return err
				}
				if err := os.RemoveAll(r.path); err != nil {
					return err
				}
				if err := workspace.Drop(ws, r.slug); err != nil {
					return err
				}
				fmt.Printf("tix spec repo: removed %s's exploration clone\n", r.slug)
			}
			return nil
		}),
	}
	reset.Flags().BoolVar(&all, "all", false, "remove every exploration clone")
	cmd.AddCommand(checkout, fetch, ls, reset)
	return cmd
}

func (r specRepo) checkout(ref string, force bool) error {
	// A new clone has nothing checked out yet (its status lists every file
	// as deleted): only an existing one can have edits
	fresh := !r.exists()
	done := false
	if fresh {
		// A clone this run made goes again if anything below fails: left
		// behind unrecorded and with nothing checked out, it would block the
		// next checkout
		defer func() {
			if !done {
				_ = fsx.RemoveAll(r.path)
			}
		}()
		if err := gitx.SharedClone(r.main, r.path); err != nil {
			return fmt.Errorf("making an exploration clone of %s failed", r.slug)
		}
	}
	r.refresh()
	if !fresh && gitx.Dirty(r.path) {
		if !force {
			return fmt.Errorf("%s has local edits; tix spec repo checkout --force %s ... discards them", r.slug, r.slug)
		}
		_ = gitx.Run(r.path, "reset", "--quiet", "--hard")
		_ = gitx.Run(r.path, "clean", "--quiet", "-fd")
	}
	if ref == "" {
		ref, _ = gitx.Out(r.path, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
		ref = strings.TrimPrefix(ref, "origin/")
		if ref == "" {
			ref, _ = gitx.Out(r.main, "rev-parse", "--abbrev-ref", "HEAD")
		}
	}
	switch {
	case gitx.HasRef(r.path, "refs/remotes/origin/"+ref):
		if err := gitx.Run(r.path, "checkout", "--quiet", "-B", ref, "origin/"+ref); err != nil {
			return err
		}
		_ = gitx.Run(r.path, "branch", "--quiet", "--set-upstream-to", "origin/"+ref)
	case gitx.HasRef(r.path, ref+"^{commit}"):
		if err := gitx.Run(r.path, "checkout", "--quiet", "--detach", ref); err != nil {
			return err
		}
	default:
		return fmt.Errorf("no branch, tag or commit '%s' in %s (tix spec repo fetch %s, or tix ws fetch %s on the host)", ref, r.slug, r.slug, r.slug)
	}
	parts := strings.SplitN(r.clone, "/", 3)
	if err := workspace.Put(r.ws, workspace.Repo{
		Provider: parts[0], Owner: parts[1], Repo: parts[2], Slug: r.slug,
		Path: r.path, Base: ref, Branch: ref, Mode: "explore",
	}); err != nil {
		return err
	}
	fmt.Printf("tix spec repo: %s at %s (%s)\n", r.slug, ref, r.lastCommit())
	fmt.Printf("tix spec repo: path %s\n", r.path)
	done = true
	return nil
}

func specLocationCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "location <slug> [repo|vault]",
		Short: "Print or record where a repo's specs live",
		Long: `Without an argument prints where the repo's specs live: "repo <dir>",
"vault <dir>", or "unset" (neither the project note's specs: field nor a
specs/ in the repo: ask the user once). With repo or vault, writes the
project note's specs: field, the answer to that question. The project note
must exist (/tix:save creates it).`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			slug := args[0]
			clone, ok := repo.Resolve(ctx.ProjectsRoot, slug)
			if !ok {
				return fmt.Errorf("no main clone '%s' (see tix ws repos)", slug)
			}
			slug = repo.Slug(clone)
			pn := spec.ProjectNote(ctx.Vault, slug)
			if len(args) == 2 {
				if args[1] != spec.InRepo && args[1] != spec.InVault {
					return fmt.Errorf("location must be repo or vault, got %q", args[1])
				}
				if _, err := os.Stat(pn); err != nil {
					return fmt.Errorf("no project note %s yet: it is created by /tix:save", config.TildePath(pn))
				}
				if err := note.Set(pn, "specs", args[1]); err != nil {
					return err
				}
			}
			loc, err := spec.Resolve(ctx.Vault, slug, filepath.Join(ctx.ProjectsRoot, clone))
			if err != nil {
				return err
			}
			if loc.Kind == spec.Unset {
				fmt.Fprintln(cmd.OutOrStdout(), "unset")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", loc.Kind, loc.Dir)
			return nil
		},
	}
}

func specCheckCmd() *cobra.Command {
	var asJSON bool
	var dir string
	cmd := &cobra.Command{
		Use:   "check [--json] [--dir <path>] [<slug>...]",
		Short: "Check specs: duplicate IDs, malformed blocks, open markers, untested requirements, unmatched interfaces",
		Long: `Reads the specs (where each repo's project note says they live, else the
repo's specs/) and reports, without changing anything:
  * duplicate requirement IDs in a repo
  * malformed requirement blocks
  * requirements with a [NEEDS CLARIFICATION] marker
  * implemented requirements no test file in the repo's main clone cites by ID
  * provides/consumes rows with no matching row in the other repo's spec
Default: every repo with specs; or name repos by slug. Exit status 1 when
there are findings. --json prints them as {"findings": [...]}.

--dir <path> checks a drafts folder laid out like specs/ (<capability>/spec.md
below it) instead, as a repo named after the folder; the repos' own specs are
still read, so its interface rows are matched against them.`,
		ValidArgsFunction: completeRepos,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			targets, err := specTargets(ctx)
			if err != nil {
				return err
			}
			var only []string
			if dir != "" {
				if len(args) > 0 {
					return errors.New("--dir takes no repo names")
				}
				abs, err := filepath.Abs(dir)
				if err != nil {
					return err
				}
				if st, err := os.Stat(abs); err != nil || !st.IsDir() {
					return fmt.Errorf("--dir %s is not a folder", dir)
				}
				slug := filepath.Base(abs)
				kept := targets[:0:0]
				for _, t := range targets {
					if t.Slug == slug {
						fmt.Fprintf(cmd.OutOrStdout(), "tix spec check: %s replaces the specs of %s\n", dir, slug)
						continue
					}
					kept = append(kept, t)
				}
				targets = append(kept, spec.Target{Slug: slug, Specs: abs})
				only = []string{slug}
			}
			for _, a := range args {
				slug := a
				if clone, ok := repo.Resolve(ctx.ProjectsRoot, a); ok {
					slug = repo.Slug(clone)
				} else if _, err := os.Stat(spec.ProjectNote(ctx.Vault, a)); err != nil {
					return fmt.Errorf("no repo '%s' (see tix ws repos)", a)
				}
				only = append(only, slug)
			}
			findings := spec.Check(targets, only)
			out := cmd.OutOrStdout()
			if asJSON {
				if findings == nil {
					findings = []spec.Finding{}
				}
				b, err := json.MarshalIndent(map[string]any{"findings": findings}, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(out, string(b))
			} else if len(findings) == 0 {
				fmt.Fprintf(out, "tix spec check: no findings (%d repos with specs)\n", len(targets))
			} else {
				for _, f := range findings {
					fmt.Fprintf(out, "[%s] %s\n", f.Repo, f)
				}
				fmt.Fprintf(out, "tix spec check: %d findings\n", len(findings))
			}
			if len(findings) > 0 {
				return exitError(1)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the findings as JSON")
	cmd.Flags().StringVar(&dir, "dir", "", "check a drafts folder laid out like specs/ instead of the repos' specs")
	return cmd
}

// specTargets lists the repos that have specs: those of the vault's
// project notes (a repo: field names the main clone) and every main clone,
// by slug, wherever Resolve says their specs live.
func specTargets(ctx vault.Context) ([]spec.Target, error) {
	clones := map[string]string{} // slug -> <provider>/<owner>/<repo>
	for _, c := range repo.MainClones(ctx.ProjectsRoot) {
		clones[repo.Slug(c)] = c
	}
	slugs := map[string]bool{}
	for s := range clones {
		slugs[s] = true
	}
	ents, _ := os.ReadDir(filepath.Join(ctx.Vault, "projects"))
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		pn := spec.ProjectNote(ctx.Vault, e.Name())
		if _, err := os.Stat(pn); err != nil {
			continue
		}
		slugs[e.Name()] = true
		if c, ok := repo.Resolve(ctx.ProjectsRoot, note.Get(pn, "repo")); ok && clones[e.Name()] == "" {
			clones[e.Name()] = c
		}
	}
	var names []string
	for s := range slugs {
		names = append(names, s)
	}
	sort.Strings(names)
	var out []spec.Target
	for _, slug := range names {
		dir := ""
		if c := clones[slug]; c != "" {
			dir = filepath.Join(ctx.ProjectsRoot, c)
		}
		loc, err := spec.Resolve(ctx.Vault, slug, dir)
		if err != nil {
			return nil, err
		}
		if loc.Kind == spec.Unset {
			continue
		}
		caps, err := spec.Capabilities(loc.Dir)
		if err != nil {
			return nil, err
		}
		if len(caps) == 0 {
			continue
		}
		out = append(out, spec.Target{Slug: slug, Dir: dir, Specs: loc.Dir})
	}
	return out, nil
}

func specDiffCmd() *cobra.Command {
	var stat bool
	cmd := &cobra.Command{
		Use:   "diff [--stat] <ID>",
		Short: "Show a ticket's spec change in every repo of its workspace",
		Long: `Prints the ticket's spec change for every repo of its workspace, wherever the
repo's specs live:
  * repo specs: the repo's specs/ against its base (from the commit the ticket
    branch left the base at), working tree included
  * vault specs: the ticket's copy under tickets/<source>/<ID>/specs/<slug>/
    against projects/<slug>/specs/; capabilities the copy lacks are unchanged.
    Once tix spec promote has moved the copy into projects/, the before side is
    tickets/<source>/<ID>/specs-base/<slug>/ and the after side that snapshot
    with the files this ticket promoted (from specs-after/<slug>/) and the
    copy laid on, so another ticket's later promotion never shows up here
Per repo: the requirement-level changes (added, changed, removed, status
flips; changed interfaces), then the raw diff. --stat prints the counts and
the changed files only.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			id := args[0]
			if !note.ValidKey(id) {
				return fmt.Errorf("invalid ticket ID %q", id)
			}
			info, err := workspace.Read(filepath.Join(ctx.WorkRoot, id))
			if err != nil {
				return fmt.Errorf("no workspace for %s: %w", id, err)
			}
			out := cmd.OutOrStdout()
			for _, r := range info.Repos {
				d, kind, err := specDiffRepo(ctx, id, r)
				if err != nil {
					return fmt.Errorf("%s: %w", r.Slug, err)
				}
				printSpecDiff(out, r.Slug, kind, d, stat)
			}
			if len(info.Repos) == 0 {
				fmt.Fprintf(out, "%s has no repos\n", id)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&stat, "stat", false, "print the counts and the changed files only, not the raw diff")
	return cmd
}

// specDiffRepo computes one repo's spec change; kind is where its specs
// live (spec.InRepo, spec.InVault, or spec.Unset for none).
func specDiffRepo(ctx vault.Context, id string, r workspace.Repo) (spec.Diff, string, error) {
	loc, err := spec.Resolve(ctx.Vault, r.Slug, r.Path)
	if err != nil {
		return spec.Diff{}, "", err
	}
	switch loc.Kind {
	case spec.InRepo:
		mb, err := spec.MergeBase(r.Path, r.Base)
		if err != nil {
			return spec.Diff{}, "", err
		}
		before, err := spec.GitTree(r.Path, mb, "specs")
		if err != nil {
			return spec.Diff{}, "", err
		}
		after, err := spec.ReadTree(loc.Dir)
		if err != nil {
			return spec.Diff{}, "", err
		}
		return spec.DiffTrees(r.Slug, before, after), loc.Kind, nil
	case spec.InVault:
		before, after, err := vaultSpecSides(ctx, id, r.Slug, loc.Dir)
		if err != nil {
			return spec.Diff{}, "", err
		}
		return spec.DiffTrees(r.Slug, before, after), loc.Kind, nil
	}
	return spec.Diff{Slug: r.Slug}, spec.Unset, nil
}

// vaultSpecSides reads a vault spec's projects/ folder, the ticket's
// snapshots, copy and version note, and gives the two sides of its change
// (spec.Sides). It refuses while the snapshots are part way rebased
// (spec.PendingRebase) or a promote of them is pending (spec.PendingPromote).
func vaultSpecSides(ctx vault.Context, id, slug, dir string) (before, after spec.Tree, err error) {
	if err := spec.Pending(ctx.Vault, id, slug); err != nil {
		return nil, nil, err
	}
	projects, err := spec.ReadTree(dir)
	if err != nil {
		return nil, nil, err
	}
	base, err := spec.ReadSnapshot(spec.BaseDir(ctx.Vault, id, slug))
	if err != nil {
		return nil, nil, err
	}
	snap, err := spec.ReadSnapshot(spec.AfterDir(ctx.Vault, id, slug))
	if err != nil {
		return nil, nil, err
	}
	copy, err := spec.ReadTree(spec.CopyDir(ctx.Vault, id, slug))
	if err != nil {
		return nil, nil, err
	}
	var promoted []string
	if v, err := spec.ReadVersion(spec.VersionPath(ctx.Vault, slug, id)); err == nil {
		promoted = v.Files
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	before, after = spec.Sides(projects, base, snap, copy, promoted)
	return before, after, nil
}

func printSpecDiff(out io.Writer, slug, kind string, d spec.Diff, stat bool) {
	switch {
	case kind == spec.Unset:
		fmt.Fprintf(out, "%s: no specs\n", slug)
		return
	case d.Empty():
		fmt.Fprintf(out, "%s (%s specs): no spec change\n", slug, kind)
		return
	}
	fmt.Fprintf(out, "%s (%s specs)\n", slug, kind)
	n := d.Counts()
	fmt.Fprintf(out, "  %d added, %d changed, %d removed, %d status\n", n[spec.Added], n[spec.Changed], n[spec.Removed], n[spec.Flipped])
	for _, s := range d.Summary() {
		fmt.Fprintf(out, "  %s\n", s)
	}
	for _, f := range d.Files {
		if stat {
			fmt.Fprintf(out, "  %s | +%d -%d\n", f.Path, f.Added, f.Removed)
		} else {
			fmt.Fprintf(out, "\n%s", f.Diff)
		}
	}
	if !stat {
		fmt.Fprintln(out)
	}
}

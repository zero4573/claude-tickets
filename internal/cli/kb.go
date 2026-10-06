package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/fsx"
	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/graph"
	"github.com/zero4573/claude-tickets/internal/repo"
	"github.com/zero4573/claude-tickets/internal/session"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

func kbCmd() *cobra.Command {
	var cont, print, noFetch bool
	cmd := &cobra.Command{
		Use:   `kb [--continue] [--print] [--no-fetch] ["<question>"]`,
		Short: "Ask about the projects the vault describes, without a ticket",
		Long: `Ask about the projects the vault describes (the current vault, ct vault
default): repos, how they work together, architecture, data flows, past
tickets, without creating a ticket.
It runs a Claude session (the kb skill) with:
  * the vault, read-write, as the knowledge base
  * every repo in its projectsRoot, read-only, freshly fetched on the host; the
    session explores branches in its own clones (ct kb repo), where it may
    build and test, but never commits or pushes
  * ct new, to turn issues it finds into manual tickets
  * one code graph (graphify) merged from all the main clones
  * the MCP servers your claude command provides

Unknowns get investigated. What's learned is drafted into the vault's
inbox/, and /tickets:save in the session promotes it into projects/,
knowledge-base/ and references/.

--print answers one question and exits (claude -p), no session; progress
is shown live and logged to ` + logPath("kb-<vault>") + `.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			question := strings.Join(args, " ")
			if print && question == "" {
				return errors.New("--print needs a question")
			}
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			return kb(ctx, question, cont, print, !noFetch)
		},
	}
	cmd.Flags().BoolVarP(&cont, "continue", "c", false, "continue the last kb conversation for this vault")
	cmd.Flags().BoolVarP(&print, "print", "p", false, "answer one question and exit")
	cmd.Flags().BoolVar(&noFetch, "no-fetch", false, "skip fetching every main clone from its remote first")
	cmd.AddCommand(kbRepoCmd())
	return cmd
}

func kb(ctx vault.Context, question string, cont, print, fetch bool) error {
	// A session may have no git credentials (e.g. sandboxed), so bring every
	// main clone's remote branches up to date here; ct kb repo fetch passes
	// them on to the session
	if fetch {
		if err := wsFetch(ctx, nil); err != nil {
			return err
		}
	}
	name := filepath.Base(ctx.Vault)
	// A workspace like a ticket's, but with no repos of its own: its graph
	// (ct graph mcp) then merges every main clone (hidden, so ct status and
	// ct ws ls skip it)
	dir := filepath.Join(ctx.WorkRoot, ".kb-"+name)
	if err := workspace.Ensure(dir, "KB", ctx.Vault); err != nil {
		return err
	}
	if err := session.Trust(dir); err != nil {
		warnf("couldn't mark %s as trusted in %s: %v", dir, session.ClaudeConfig(), err)
	}
	// Identifies this session's inbox/ drafts for /tickets:save
	sessionID := "kb-" + name + "-" + time.Now().Format("20060102-1504")
	// The exploration clones (in this directory) are the session's own, so it
	// may edit, build and test in them. The main clones stay untouched, and
	// nothing is committed or pushed.
	if err := session.WriteSettings(dir, ctx.ProjectsRoot, "Bash(git commit:*)", "Bash(git push:*)"); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(kbClaudeMD(ctx, dir, sessionID)), 0o644); err != nil {
		return err
	}

	argv, err := session.Command(ctx.Vault, ctx.ProjectsRoot, filepath.Join(config.CacheDir(), "graphify"))
	if err != nil {
		return err
	}
	if _, err := graph.Build(false); err == nil {
		mcp, err := session.WriteGraphMCP(dir, ctx.Vault)
		if err != nil {
			return err
		}
		argv = append(argv, "--mcp-config", mcp)
	} else {
		warnf("%v", err)
		warnf("the graphify image couldn't be built; starting without the code graph")
	}
	argv = append(argv, "--permission-mode", "auto")
	if cont {
		argv = append(argv, "--continue")
	}
	switch {
	case print:
		argv = append(argv, "-p", "/tickets:kb "+question, "--output-format", "stream-json", "--verbose")
	case question != "":
		argv = append(argv, "/tickets:kb "+question)
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	if !print {
		path, err := exec.LookPath(argv[0])
		if err != nil {
			return err
		}
		// So ct ws gc sees the session running while its process lives
		if err := workspace.RecordSession(dir, "kb"); err != nil {
			warnf("couldn't record the session in %s: %v", workspace.SessionsFile(dir), err)
		}
		return passExit(execReplaceOrRun(path, argv))
	}
	// One-shot answer: progress as it happens, also logged
	log := config.StateLog("kb-" + name)
	if f, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintf(f, "=== kb %s vault=%s: %s\n", time.Now().Format(time.RFC3339), ctx.Vault, question)
		f.Close()
	}
	return passExit(session.Headless(log, argv))
}

func kbClaudeMD(ctx vault.Context, dir, sessionID string) string {
	return fmt.Sprintf("# Knowledge-base session\n\n"+
		"Written by ct kb; regenerated on every start, so don't edit it.\n\n"+
		"- Vault (knowledge base, read-write): `%[1]s`. Read its `AGENTS.md`.\n"+
		"- Repos: main clones at `%[2]s/<provider>/<owner>/<repo>`, read-only,\n"+
		"  fetched from their remotes when this session started. `ct ws repos`\n"+
		"  lists every repo with its slug.\n"+
		"- **Exploring branches:** `ct kb repo checkout <repo> [<branch>|<tag>|<commit>]`\n"+
		"  gives you an exploration clone at `%[3]s/<slug>` on that ref. The code graph\n"+
		"  swaps it in for the main clone and rebuilds it as the checkout changes.\n"+
		"  `ct kb repo fetch` refreshes remote branches, `ct kb repo graph <repo>` forces a\n"+
		"  rebuild, `ct kb repo ls` shows what's checked out, `ct kb repo reset` drops\n"+
		"  clones.\n"+
		"- **Experiments are fine, changes aren't kept:** in your exploration clones\n"+
		"  you may edit, build, run tests and add debug output to check how something\n"+
		"  behaves. Those edits stay in this session: never commit or push (both\n"+
		"  denied), and `ct kb repo checkout --force` discards them. The main clones in\n"+
		"  `%[2]s` are never edited.\n"+
		"- **Issues become tickets:** when you find something that should be\n"+
		"  investigated or fixed, create a manual ticket with\n"+
		"  `ct new \"<summary>\"` and fill it in (see the `kb`\n"+
		"  skill), so the work goes through the ticket workflow.\n"+
		"- `ct vault groups %[1]s` shows which projects work together (groups,\n"+
		"  `depends-on`) and which are unrelated.\n"+
		"- Code graph: the `graphify` MCP server holds every repo, with node ids\n"+
		"  prefixed by the repo's slug (`bitbucket-acme-billing-service::…`).\n"+
		"- Follow the `kb` skill: answer from the vault, then the graph, then the\n"+
		"  code. Investigate unknowns, draft what you learn into `%[1]s/inbox/`,\n"+
		"  and run `/tickets:save` before the session ends to promote the drafts.\n"+
		"- Session id (put it in the `session:` field of your inbox/ drafts):\n"+
		"  `%[4]s`\n",
		ctx.Vault, ctx.ProjectsRoot, dir, sessionID)
}

// kbRepo is an exploration clone of a kb session.
type kbRepo struct {
	ctx               vault.Context
	dir, ws           string // the kb workspace and its workspace.json
	clone, slug, main string
	path              string
}

// kbWorkspace finds the kb workspace: $KB_DIR, else the nearest folder up
// from here whose workspace.json is the kb's (so it also works from inside
// an exploration clone).
func kbWorkspace() (string, error) {
	start := os.Getenv("KB_DIR")
	if start == "" {
		start, _ = os.Getwd()
	}
	for d := start; ; d = filepath.Dir(d) {
		if info, err := workspace.Read(d); err == nil && info.ID == "KB" {
			return d, nil
		}
		if d == filepath.Dir(d) {
			return "", fmt.Errorf("not in a kb session (no kb workspace.json above %s)", start)
		}
	}
}

func kbRepoOf(ctx vault.Context, dir, spec string) (kbRepo, error) {
	clone, ok := repo.Resolve(ctx.ProjectsRoot, spec)
	if !ok {
		return kbRepo{}, fmt.Errorf("no main clone '%s' (see ct ws repos)", spec)
	}
	slug := repo.Slug(clone)
	return kbRepo{ctx, dir, workspace.File(dir), clone, slug, filepath.Join(ctx.ProjectsRoot, clone), filepath.Join(dir, slug)}, nil
}

// refresh copies remote branches and tags from the main clone, then from
// the remote itself where it can be reached.
func (r kbRepo) refresh() {
	_ = gitx.RefreshSharedClone(r.main, r.path)
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	git := exec.CommandContext(c, "git", "-C", r.path, "fetch", "--quiet", "--prune", "origin")
	// Never prompt: no credentials, no host-key questions, just fail fast
	git.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=10")
	if git.Run() != nil {
		fmt.Fprintf(os.Stderr, "ct kb repo: %s: can't reach origin from here; using the main clone's fetch (run ct ws fetch %s on the host for newer)\n", r.slug, r.slug)
	}
}

func (r kbRepo) lastCommit() string {
	s, _ := gitx.Out(r.path, "log", "-1", "--format=%h %cs %s")
	return s
}

func (r kbRepo) exists() bool { return fsx.IsDir(filepath.Join(r.path, ".git")) }

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

func kbRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Exploration clones of a kb session (inside one)",
		Long: `Exploration clones for a kb session: look at any branch or commit of a repo
without touching its main clone (read-only here). Each is a shared clone (it
borrows the main clone's objects) at <workRoot>/.kb-<vault>/<slug>, registered in the session's workspace.json, so
the session's code graph swaps it in for the main clone and rebuilds it as
the checkout changes. A <repo> is a slug or <provider>/<owner>/<repo>.

The clones are the session's own: edit, build and test in them as needed to
check how things behave, but never commit or push (both denied). Edits stay
in the session; checkout --force or reset throws them away.`,
	}
	with := func(fn func(ctx vault.Context, dir string, args []string) error) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, args []string) error {
			dir, err := kbWorkspace()
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
			r, err := kbRepoOf(ctx, dir, args[0])
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
				r, err := kbRepoOf(ctx, dir, a)
				if err != nil {
					return err
				}
				if !r.exists() {
					warnf("%s has no exploration clone (ct kb repo checkout %s)", r.slug, r.slug)
					continue
				}
				r.refresh()
				if up, err := gitx.Out(r.path, "rev-parse", "--abbrev-ref", "@{u}"); err == nil && !gitx.Dirty(r.path) {
					if gitx.Timeout(time.Minute, r.path, "merge", "--quiet", "--ff-only", up) != nil {
						warnf("%s: can't fast-forward to %s", r.slug, up)
					}
				}
				fmt.Printf("ct kb repo: %s fetched (%s)\n", r.slug, r.lastCommit())
			}
			return nil
		}),
	}
	graphC := &cobra.Command{
		Use:               "graph <repo>",
		Short:             "Rebuild the repo's code graph now",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeRepos,
		RunE: with(func(ctx vault.Context, dir string, args []string) error {
			r, err := kbRepoOf(ctx, dir, args[0])
			if err != nil {
				return err
			}
			if !r.exists() {
				return fmt.Errorf("%s has no exploration clone (ct kb repo checkout %s)", r.slug, r.slug)
			}
			out := filepath.Join(r.path, "graphify-out")
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}
			now := time.Now()
			if err := os.WriteFile(filepath.Join(out, ".rebuild"), nil, 0o644); err != nil {
				return err
			}
			_ = os.Chtimes(filepath.Join(out, ".rebuild"), now, now)
			fmt.Printf("ct kb repo: asked the graph sidecar to rebuild %s\n", r.slug)
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
		Short:             "Remove exploration clones; the graph falls back to the main clone",
		ValidArgsFunction: completeRepos,
		RunE: with(func(ctx vault.Context, dir string, args []string) error {
			ws := workspace.File(dir)
			if all {
				args = exploreSlugs(ws)
			} else if len(args) != 1 {
				return errors.New("reset: name a <repo>, or --all")
			}
			for _, a := range args {
				r, err := kbRepoOf(ctx, dir, a)
				if err != nil {
					return err
				}
				if err := os.RemoveAll(r.path); err != nil {
					return err
				}
				if err := workspace.Drop(ws, r.slug); err != nil {
					return err
				}
				fmt.Printf("ct kb repo: removed %s's exploration clone\n", r.slug)
			}
			return nil
		}),
	}
	reset.Flags().BoolVar(&all, "all", false, "remove every exploration clone")
	cmd.AddCommand(checkout, fetch, graphC, ls, reset)
	return cmd
}

func (r kbRepo) checkout(ref string, force bool) error {
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
		graph.Seed(r.main, r.path)
	}
	r.refresh()
	if !fresh && gitx.Dirty(r.path) {
		if !force {
			return fmt.Errorf("%s has local edits; ct kb repo checkout --force %s ... discards them", r.slug, r.slug)
		}
		_ = gitx.Run(r.path, "reset", "--quiet", "--hard")
		_ = gitx.Run(r.path, "clean", "--quiet", "-fd", "-e", "graphify-out/")
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
		return fmt.Errorf("no branch, tag or commit '%s' in %s (ct kb repo fetch %s, or ct ws fetch %s on the host)", ref, r.slug, r.slug, r.slug)
	}
	parts := strings.SplitN(r.clone, "/", 3)
	if err := workspace.Put(r.ws, workspace.Repo{
		Provider: parts[0], Owner: parts[1], Repo: parts[2], Slug: r.slug,
		Path: r.path, Base: ref, Branch: ref, Mode: "explore",
	}); err != nil {
		return err
	}
	fmt.Printf("ct kb repo: %s at %s (%s)\n", r.slug, ref, r.lastCommit())
	fmt.Printf("ct kb repo: path %s; the code graph picks it up within ~15s\n", r.path)
	done = true
	return nil
}

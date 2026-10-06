package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/followup"
	"github.com/zero4573/claude-tickets/internal/fsx"
	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/graph"
	"github.com/zero4573/claude-tickets/internal/launcher"
	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/repo"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

func wsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ws",
		Short: "Git worktrees of ticket workspaces",
		Long: `Manages the git worktrees of the current vault's ticket workspaces
(<workRoot>/<ID>; ct vault shows the vault's workRoot and projectsRoot, by
default ~/Projects/work-<vault> and ~/Projects/repo-<vault>). Main clones
live at <projectsRoot>/<provider>/<owner>/<repo>, and each has a slug,
<provider>-<owner>-<repo> (e.g. bitbucket-acme-billing-service), which is
how the vault and the code graph name it. A ticket's worktree of a repo is
<workRoot>/<ID>/<slug>, on branch feature/<ID>[-<description>]. Each
workspace's repos are recorded in <workRoot>/<ID>/workspace.json.

A <repo> argument is a slug or <provider>/<owner>/<repo>.`,
	}
	cmd.AddCommand(wsReposCmd(), wsCloneCmd(), wsAddCmd(), wsLsCmd(), wsDiffCmd(),
		wsRmCmd(), wsSignCmd(), wsFetchCmd(), wsGcCmd())
	return cmd
}

func warnf(format string, a ...any) { fmt.Fprintf(os.Stderr, "ct: "+format+"\n", a...) }

func completeRepos(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	ctx := vault.Optional()
	var out []string
	for _, c := range repo.MainClones(ctx.ProjectsRoot) {
		out = append(out, repo.Slug(c)+"\t"+c)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func completeWorkspaceID(cmd *cobra.Command, args []string, s string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeIDs(workspaceTickets)(cmd, args, s)
}

func requireKey(cmd, key string) error {
	if !note.ValidKey(key) {
		return fmt.Errorf("%s: not a ticket ID: '%s'", cmd, key)
	}
	return nil
}

// wsRepos is empty for a workspace without a workspace.json.
func wsRepos(ctx vault.Context, key string) []workspace.Repo {
	info, _ := workspace.Read(filepath.Join(ctx.WorkRoot, key))
	return info.Repos
}

func table() *tabwriter.Writer { return tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0) }

func wsReposCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "repos",
		Short: "List the main clones: slug and <provider>/<owner>/<repo>",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := vault.Optional()
			w := table()
			fmt.Fprintln(w, "SLUG\tREPO")
			for _, c := range repo.MainClones(ctx.ProjectsRoot) {
				fmt.Fprintf(w, "%s\t%s\n", repo.Slug(c), c)
			}
			return w.Flush()
		},
	}
}

func wsCloneCmd() *cobra.Command {
	var url string
	cmd := &cobra.Command{
		Use:   "clone <provider>/<owner>/<repo> | --url <git url>",
		Short: "Clone a repo into <projectsRoot>/<provider>/<owner>/<repo> and build its code graph",
		Long: `Clones a repo into <projectsRoot>/<provider>/<owner>/<repo> and builds its
code graph. Run it on the host: a sandboxed session may have no git credentials.
The default URL is SSH (git@bitbucket.org:<owner>/<repo>.git, with
CLAUDE_TICKETS_BITBUCKET_HOST for another Bitbucket host, or the GitHub
equivalent). A running ticket session can add the new repo straight away:
ct ws add makes a shared clone when it can't write to the main clone's .git.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := vault.Optional()
			if url == "" {
				spec := ""
				if len(args) == 1 {
					spec = args[0]
				}
				provider, rest, _ := strings.Cut(spec, "/")
				if strings.Count(spec, "/") < 2 {
					return errors.New("clone: give <provider>/<owner>/<repo> (or --url <git url>)")
				}
				switch provider {
				case "bitbucket":
					host := os.Getenv("CLAUDE_TICKETS_BITBUCKET_HOST")
					if host == "" {
						host = "bitbucket.org"
					}
					url = "git@" + host + ":" + rest + ".git"
				case "github":
					url = "git@github.com:" + rest + ".git"
				default:
					return fmt.Errorf("clone: no default URL for provider '%s'; pass --url <git url>", provider)
				}
			}
			id, ok := repo.RemoteIdentity(url)
			if !ok {
				return fmt.Errorf("clone: can't tell provider/owner/repo from '%s'", url)
			}
			clone := id.Clone()
			dest := filepath.Join(ctx.ProjectsRoot, clone)
			if st, err := os.Stat(filepath.Join(dest, ".git")); err == nil && st.IsDir() {
				fmt.Printf("ct ws: %s is already cloned at %s\n", clone, dest)
				return nil
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			git := exec.Command("git", "clone", url, dest)
			git.Stdin, git.Stdout, git.Stderr = os.Stdin, os.Stdout, os.Stderr
			if git.Run() != nil {
				return errors.New("clone: git clone failed (a session may have no git credentials: run this on the host)")
			}
			fmt.Printf("ct ws: cloned %s (slug %s)\n", clone, repo.Slug(clone))
			if err := indexGraphs(ctx, []string{clone}); err != nil {
				warnf("ct graph index failed for %s; run it again later", clone)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "clone this git URL")
	return cmd
}

func wsAddCmd() *cobra.Command {
	var base, desc, version, useBranch string
	cmd := &cobra.Command{
		Use:   "add <ID> <repo> --base <branch> [--desc <description>] [--version <target>] [--branch <existing branch>]",
		Short: "Create (or reuse) a ticket's worktree of a repo",
		Long: `Creates (or reuses) the ticket's worktree of a repo, branched from
origin/<base> (or the local <base> when there's no such remote branch). The
branch is reused if it already exists. Copies untracked files matching the
main clone's .worktreeinclude (gitignore syntax, e.g. .env) and seeds
graphify-out/ from the main clone so the code graph builds incrementally.
--branch checks out an existing branch instead (local, or origin's,
tracked), e.g. the source branch of an open PR; --base is then the branch
it merges into.`,
		Args: cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, s string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeIDs(openTickets)(cmd, args, s)
			}
			if len(args) == 1 {
				return completeRepos(cmd, args, s)
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			return wsAdd(ctx, args[0], args[1], base, desc, version, useBranch)
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "the branch it starts from and merges into (required)")
	cmd.Flags().StringVar(&desc, "desc", "", "description for the branch name (feature/<ID>-<description>)")
	cmd.Flags().StringVar(&version, "version", "", "the release the change targets (recorded in workspace.json)")
	cmd.Flags().StringVar(&useBranch, "branch", "", "check out this existing branch instead")
	return cmd
}

func wsAdd(ctx vault.Context, key, spec, base, desc, version, useBranch string) error {
	if err := requireKey("add", key); err != nil {
		return err
	}
	if base == "" {
		return errors.New("add: --base <branch> is required")
	}
	clone, ok := repo.Resolve(ctx.ProjectsRoot, spec)
	if !ok {
		return fmt.Errorf("add: no main clone '%s' (see ct ws repos)", spec)
	}
	slug := repo.Slug(clone)
	main := filepath.Join(ctx.ProjectsRoot, clone)
	dir := filepath.Join(ctx.WorkRoot, key)
	wt := filepath.Join(dir, slug)
	branch := "feature/" + key
	if desc != "" {
		branch += "-" + repo.Kebab(desc)
	}
	if useBranch != "" {
		branch = useBranch
	}
	if err := workspace.Ensure(dir, key, ""); err != nil {
		return err
	}
	var mode string
	if st, err := os.Stat(filepath.Join(wt, ".git")); err == nil {
		branch, _ = gitx.Out(wt, "rev-parse", "--abbrev-ref", "HEAD")
		mode = "worktree"
		if st.IsDir() {
			mode = "clone"
		}
		fmt.Printf("ct ws: %s already exists (branch %s)\n", wt, branch)
	} else {
		// Best effort: a session may have no git credentials (e.g. sandboxed),
		// so this may only work on the host (ct start fetches before launching)
		if gitx.Timeout(30*time.Second, main, "fetch", "--quiet", "origin", base) != nil {
			warnf("couldn't fetch origin/%s in %s, using what's already fetched", base, slug)
		}
		start := gitx.BaseRef(main, base)
		if !gitx.HasRef(main, start+"^{commit}") {
			return fmt.Errorf("add: base branch '%s' not found in %s", base, slug)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if useBranch == "" && desc == "" {
			existing, _ := gitx.Out(main, "for-each-ref", "--format=%(refname:short)",
				"refs/heads/feature/"+key, "refs/heads/feature/"+key+"-*")
			if first, _, _ := strings.Cut(existing, "\n"); first != "" {
				branch = first
			}
		}
		// What the branch starts from: an existing local branch, an existing
		// remote one (e.g. an open PR's source), or a new branch off the base.
		// Upstreams aren't set here (that writes the main clone's .git/config,
		// which sandboxes mount read-only); push.autoSetupRemote covers the push.
		from := "new"
		var commit string
		switch {
		case gitx.HasRef(main, "refs/heads/"+branch):
			from = "local"
			commit, _ = gitx.Out(main, "rev-parse", "refs/heads/"+branch)
		case useBranch != "":
			if !gitx.HasRef(main, "refs/remotes/origin/"+branch) {
				return fmt.Errorf("add: branch '%s' not found locally or on origin in %s (fetch on the host: ct ws fetch %s)", branch, slug, slug)
			}
			from = "remote"
			commit, _ = gitx.Out(main, "rev-parse", "refs/remotes/origin/"+branch)
		default:
			commit, _ = gitx.Out(main, "rev-parse", start+"^{commit}")
		}

		// CLAUDE_TICKETS_WS_SHARED_CLONE=1 forces a shared clone (testing)
		if gitx.Writable(filepath.Join(main, ".git", "refs")) && os.Getenv("CLAUDE_TICKETS_WS_SHARED_CLONE") == "" {
			mode = "worktree"
			if from == "local" {
				err = gitx.Run(main, "worktree", "add", "--quiet", wt, branch)
			} else {
				err = gitx.Run(main, "worktree", "add", "--quiet", "--no-track", "-b", branch, wt, commit)
			}
			if err != nil {
				return fmt.Errorf("add: git worktree add failed in %s", slug)
			}
		} else {
			// The main clone's .git is read-only here: a repo cloned on the host
			// after this ticket's sandbox started isn't in its read-write mounts.
			// A shared clone writes nothing to the main clone, and its origin is
			// the real remote, so the user's push from it goes to the server.
			mode = "clone"
			if err := gitx.SharedClone(main, wt); err != nil {
				_ = fsx.RemoveAll(wt)
				return fmt.Errorf("add: making a shared clone of %s failed", slug)
			}
			if err := gitx.Run(wt, "checkout", "--quiet", "--no-track", "-B", branch, commit); err != nil {
				// (left behind, the next add would take it for a finished checkout)
				_ = fsx.RemoveAll(wt)
				return fmt.Errorf("add: checking out %s failed in %s", branch, wt)
			}
			if gitx.HasRef(wt, "refs/remotes/origin/"+branch) {
				_ = gitx.Run(wt, "branch", "--quiet", "--set-upstream-to", "origin/"+branch)
			}
		}
		if from == "new" {
			fmt.Printf("ct ws: created %s (%s) on %s (from %s)\n", wt, mode, branch, start)
		} else {
			fmt.Printf("ct ws: created %s (%s) on existing branch %s\n", wt, mode, branch)
		}
		copyWorktreeIncludes(main, wt)
		graph.Seed(main, wt)
	}
	parts := strings.SplitN(clone, "/", 3)
	return workspace.Put(workspace.File(dir), workspace.Repo{
		Provider: parts[0], Owner: parts[1], Repo: parts[2], Slug: slug,
		Path: wt, Base: base, Branch: branch, Mode: mode, TargetVersion: &version,
	})
}

// copyWorktreeIncludes copies the untracked, ignored files a checkout
// needs (.env, local config) that match the main clone's .worktreeinclude.
func copyWorktreeIncludes(main, wt string) {
	if _, err := os.Stat(filepath.Join(main, ".worktreeinclude")); err != nil {
		return
	}
	out, err := gitx.Out(main, "ls-files", "-z", "--others", "--ignored", "--exclude-from=.worktreeinclude")
	if err != nil {
		return
	}
	for _, f := range strings.Split(out, "\x00") {
		if f == "" {
			continue
		}
		if err := fsx.CopyFile(filepath.Join(main, f), filepath.Join(wt, f)); err != nil {
			warnf("couldn't copy %s into %s: %v", f, wt, err)
		}
	}
}

func wsLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "ls [<ID>]",
		Short:             "Worktrees per ticket: branch, uncommitted changes, ahead/behind the base",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeWorkspaceID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			var keys []string
			if len(args) == 1 {
				keys = args
			} else {
				for _, d := range workspace.List(ctx.WorkRoot, false) {
					keys = append(keys, filepath.Base(d))
				}
			}
			w := table()
			fmt.Fprintln(w, "TICKET\tREPO\tBRANCH\tBASE\tCHANGED\tAHEAD\tBEHIND")
			for _, key := range keys {
				for _, r := range wsRepos(ctx, key) {
					if _, err := os.Stat(filepath.Join(r.Path, ".git")); err != nil {
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\tmissing\t-\t-\n", key, r.Slug, r.Branch, r.Base)
						continue
					}
					status, _ := gitx.Out(r.Path, "status", "--porcelain")
					changed := 0
					if status != "" {
						changed = strings.Count(status, "\n") + 1
					}
					behind, ahead := "?", "?"
					if counts, err := gitx.Out(r.Path, "rev-list", "--left-right", "--count", gitx.BaseRef(r.Path, r.Base)+"...HEAD"); err == nil {
						if f := strings.Fields(counts); len(f) == 2 {
							behind, ahead = f[0], f[1]
						}
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n", key, r.Slug, r.Branch, r.Base, changed, ahead, behind)
				}
			}
			return w.Flush()
		},
	}
}

func wsDiffCmd() *cobra.Command {
	var stat bool
	cmd := &cobra.Command{
		Use:   "diff <ID> [--stat]",
		Short: "Everything changed on a ticket's branches since they left their base",
		Long: `Everything changed on the ticket's branches since they left their base,
committed or not, plus untracked files, per repo.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeWorkspaceID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			key := args[0]
			if err := requireKey("diff", key); err != nil {
				return err
			}
			for _, r := range wsRepos(ctx, key) {
				fmt.Printf("=== %s (%s vs %s) ===\n", r.Slug, r.Branch, r.Base)
				if _, err := os.Stat(filepath.Join(r.Path, ".git")); err != nil {
					fmt.Printf("(worktree missing: %s)\n", r.Path)
					continue
				}
				mb, err := gitx.Out(r.Path, "merge-base", gitx.BaseRef(r.Path, r.Base), "HEAD")
				if err != nil || mb == "" {
					mb = "HEAD"
				}
				diff := []string{"--no-pager", "diff"}
				if stat {
					diff = append(diff, "--stat")
				}
				_ = gitx.Run(r.Path, append(diff, mb)...)
				if untracked, _ := gitx.Out(r.Path, "ls-files", "--others", "--exclude-standard"); untracked != "" {
					fmt.Println("--- untracked files:")
					fmt.Println(untracked)
				}
				fmt.Println()
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&stat, "stat", false, "only a diffstat")
	return cmd
}

func wsRmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <ID> [--force]",
		Short: "Remove a ticket's worktrees and workspace files (branches are kept)",
		Long: `Removes the ticket's worktrees (refusing dirty ones unless --force) and its
workspace files. Branches are kept. A shared clone (see ct ws clone) is
deleted, and refused while it has commits that aren't on origin.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeWorkspaceID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			return wsRm(ctx, args[0], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "remove dirty worktrees and unpushed shared clones too")
	return cmd
}

func wsRm(ctx vault.Context, key string, force bool) error { return wsRemove(ctx, key, force, false) }

// wsRemove is wsRm; merged also lets a shared clone go when its unpushed
// commits are already in origin/<base> (gitx.Lost: e.g. squash-merged, the
// branch deleted on the server), as ct clean judges it.
func wsRemove(ctx vault.Context, key string, force, merged bool) error {
	if err := requireKey("rm", key); err != nil {
		return err
	}
	dir := filepath.Join(ctx.WorkRoot, key)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return fmt.Errorf("rm: no workspace at %s", dir)
	}
	failed := false
	for _, r := range wsRepos(ctx, key) {
		main := filepath.Join(ctx.ProjectsRoot, r.Clone())
		if st, err := os.Stat(filepath.Join(r.Path, ".git")); err == nil {
			if !force && gitx.Dirty(r.Path, "--ignore-submodules") {
				warnf("%s has uncommitted changes, not removing (use --force)", r.Slug)
				failed = true
				continue
			}
			if st.IsDir() {
				// A shared clone (see add): its branches live only in it, so refuse
				// to drop work that was never pushed (or that git can't vouch for)
				unpushed, err := gitx.Unpushed(r.Path)
				if merged && (err != nil || unpushed) {
					unpushed, err = gitx.Lost(r.Path, r.Base)
				}
				if !force && (err != nil || unpushed) {
					warnf("%s has commits that aren't on origin or stashed changes, not removing (push them, or use --force)", r.Slug)
					failed = true
					continue
				}
				if err := os.RemoveAll(r.Path); err != nil {
					return err
				}
				fmt.Printf("ct ws: removed %s (shared clone)\n", r.Path)
				continue
			}
			// graphify-out is ignored, so `git worktree remove` would refuse it
			_ = os.RemoveAll(filepath.Join(r.Path, "graphify-out"))
			args := []string{"worktree", "remove"}
			if force {
				args = append(args, "--force")
			}
			if err := gitx.Run(main, append(args, r.Path)...); err != nil {
				warnf("couldn't remove the worktree %s", r.Path)
				failed = true
				continue
			}
			fmt.Printf("ct ws: removed %s (branch %s kept)\n", r.Path, r.Branch)
		}
		_ = gitx.Timeout(time.Minute, main, "worktree", "prune")
	}
	if failed {
		return fmt.Errorf("rm: some worktrees were kept; workspace %s left in place", dir)
	}
	for _, f := range []string{"graphify-out", ".claude", "workspace.json", "workspace.json.lock",
		"CLAUDE.md", ".agent-state", ".sessions.json", ".sessions.json.lock", key + ".code-workspace"} {
		_ = os.RemoveAll(filepath.Join(dir, f))
	}
	fsx.RemoveEmptyDirs(dir)
	if _, err := os.Stat(dir); err == nil {
		warnf("left %s in place: it still has other files", dir)
	} else {
		fmt.Printf("ct ws: removed workspace %s\n", dir)
	}
	return nil
}

func wsSignCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sign <ID>",
		Short: "On the host: sign a ticket's unpushed commits",
		Long: `On the host: signs the ticket's commits. Sessions commit on the ticket
branches but may not be able to sign (no SSH agent in a sandbox), so this
replays each worktree's unpushed commits onto the same parent with
--gpg-sign (the key the repo's git identity sets). Same changes and
authors, new hashes; commits already on a remote are left alone. Run it
before pushing.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeWorkspaceID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			key := args[0]
			if err := requireKey("sign", key); err != nil {
				return err
			}
			if gitx.InContainer() {
				return errors.New("sign: run it on the host, where your signing key is")
			}
			if _, err := os.Stat(workspace.File(filepath.Join(ctx.WorkRoot, key))); err != nil {
				return fmt.Errorf("sign: %s has no workspace (%s)", key, filepath.Join(ctx.WorkRoot, key))
			}
			failed := false
			for _, r := range wsRepos(ctx, key) {
				if !signRepo(r) {
					failed = true
				}
			}
			if failed {
				return exitError(1)
			}
			return nil
		},
	}
}

// signRepo re-signs one checkout's unpushed commits; false on failure.
func signRepo(r workspace.Repo) bool {
	p := r.Path
	if _, err := os.Stat(filepath.Join(p, ".git")); err != nil {
		warnf("sign: %s: worktree missing (%s)", r.Slug, p)
		return true
	}
	if out, _ := gitx.Out(p, "rev-list", "-n1", "HEAD", "--not", "--remotes"); out == "" {
		fmt.Printf("ct ws: %s: no unpushed commits on %s\n", r.Slug, r.Branch)
		return true
	}
	// From the oldest unpushed commit without a signature (%G? N); the ones
	// before it are already signed and keep their hashes
	log, _ := gitx.Out(p, "log", "--reverse", "--topo-order", "--no-show-signature", "--format=%H %G?", "HEAD", "--not", "--remotes")
	first := ""
	for _, line := range strings.Split(log, "\n") {
		if h, s, _ := strings.Cut(line, " "); s == "N" {
			first = h
			break
		}
	}
	if first == "" {
		fmt.Printf("ct ws: %s: unpushed commits on %s are already signed\n", r.Slug, r.Branch)
		return true
	}
	n, err := gitx.Out(p, "rev-list", "--count", "HEAD", "^"+first+"^", "--not", "--remotes")
	if err != nil {
		n, _ = gitx.Out(p, "rev-list", "--count", "HEAD", "--not", "--remotes")
	}
	onto := []string{"--root"}
	if parent, err := gitx.Out(p, "rev-parse", "-q", "--verify", first+"^"); err == nil && parent != "" {
		onto = []string{parent}
	}
	if err := gitx.Run(p, append([]string{"rebase", "--quiet", "--force-rebase", "--rebase-merges", "--autostash", "--gpg-sign"}, onto...)...); err != nil {
		_ = gitx.Timeout(time.Minute, p, "rebase", "--abort")
		warnf("sign: %s: couldn't re-sign %s (see above); left as it was", r.Slug, r.Branch)
		return false
	}
	sigs, _ := gitx.Out(p, "log", "--no-show-signature", "--format=%G?", "HEAD", "--not", "--remotes")
	unsigned := 0
	for _, s := range strings.Split(sigs, "\n") {
		if s == "N" {
			unsigned++
		}
	}
	if unsigned > 0 {
		warnf("sign: %s: %d of %s commit(s) on %s still unsigned (no signing key for this repo's identity?)", r.Slug, unsigned, n, r.Branch)
		return false
	}
	fmt.Printf("ct ws: %s: signed %s commit(s) on %s\n", r.Slug, n, r.Branch)
	return true
}

func wsFetchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fetch [<repo>...]",
		Short: "Fetch origin in the main clones and pass the refs on to shared clones",
		Long: `Fetches origin in the given main clones (default: all), in parallel, then
passes the new refs on to the shared clones in the vault's ticket and kb
workspaces. Other vaults' shared clones get them on their own fetch.`,
		ValidArgsFunction: completeRepos,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			return wsFetch(ctx, args)
		},
	}
}

func wsFetch(ctx vault.Context, specs []string) error {
	var repos []string
	for _, s := range specs {
		c, ok := repo.Resolve(ctx.ProjectsRoot, s)
		if !ok {
			return fmt.Errorf("fetch: no main clone '%s' (see ct ws repos)", s)
		}
		repos = append(repos, c)
	}
	if len(repos) == 0 {
		repos = repo.MainClones(ctx.ProjectsRoot)
	}
	if len(repos) == 0 {
		return nil
	}
	fmt.Fprintf(os.Stderr, "ct ws: fetching %d repo(s)...\n", len(repos))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, c := range repos {
		wg.Add(1)
		sem <- struct{}{}
		go func(c string) {
			defer func() { <-sem; wg.Done() }()
			if gitx.Timeout(60*time.Second, filepath.Join(ctx.ProjectsRoot, c), "fetch", "--quiet", "--prune", "origin") != nil {
				fmt.Fprintf(os.Stderr, "ct ws: fetch failed: %s\n", c)
			}
		}(c)
	}
	wg.Wait()
	// Shared clones (ticket and kb workspaces) copy their refs from the main
	// clone, so pass the new ones on
	for _, dir := range workspace.List(ctx.WorkRoot, true) {
		info, _ := workspace.Read(dir)
		for _, r := range info.Repos {
			if r.Mode != "clone" && r.Mode != "explore" {
				continue
			}
			main := filepath.Join(ctx.ProjectsRoot, r.Clone())
			if !fsx.IsDir(filepath.Join(r.Path, ".git")) || !fsx.IsDir(filepath.Join(main, ".git")) {
				continue
			}
			if gitx.RefreshSharedClone(main, r.Path) != nil {
				warnf("couldn't refresh %s from its main clone", r.Path)
			}
		}
	}
	return nil
}

func wsGcCmd() *cobra.Command {
	var days int
	var dry bool
	cmd := &cobra.Command{
		Use:   "gc [--days N] [--dry-run]",
		Short: "Free disk: finished workspaces, idle merged graphs and kb clones, old snapshots",
		Long: `Frees disk in the current vault: removes workspaces of tickets that are
done or closed and idle for N days (default 14; dirty or unpushed work is
kept, and left as a follow-up task in the vault's inbox), merged graphs of
sessions that aren't running, idle kb exploration clones, and graphify's
dated snapshots older than a week.

A session counts as running when its window is open (tmux), else when its
process (ct start without a multiplexer, ct kb) is alive on this host,
else, when that can't be checked, when its hook state (.agent-state) is
less than a day old and not exited. Without a terminal multiplexer
(CLAUDE_TICKETS_LAUNCHER=none, or no tmux installed) there are no windows,
so only the last two apply.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if days < 0 {
				return errors.New("gc: --days must be a number")
			}
			// It tells running sessions apart by the vault's tmux session and
			// the sessions' PIDs; inside a container it would see neither and
			// clean up live workspaces
			if gitx.InContainer() {
				return errors.New("gc: run it on the host, not inside a container")
			}
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			l, err := launcher.Resolve()
			if err != nil {
				return err
			}
			return wsGc(ctx, l, days, dry)
		},
	}
	cmd.Flags().IntVar(&days, "days", 14, "idle days before a finished workspace or kb clone goes")
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "only say what would go")
	return cmd
}

func wsGc(ctx vault.Context, l launcher.Launcher, days int, dry bool) error {
	act := ""
	if dry {
		act = "would "
	}
	// Workspaces kept although their ticket is finished, per vault: tasks
	// for a follow-up note in that vault's inbox/
	kept := map[string][]string{}
	for _, dir := range workspace.List(ctx.WorkRoot, true) {
		key := filepath.Base(dir)
		kb := strings.HasPrefix(key, ".kb-")
		ws := workspace.File(dir)
		running := workspace.Running(dir, l, ctx.TmuxSession)

		if !kb && !running && fsx.OlderThanDays(ws, days) {
			info, _ := workspace.Read(dir)
			status := ""
			if info.Vault != "" {
				status = note.Get(note.TicketPath(info.Vault, key), "status")
			}
			if status == "done" || status == "closed" {
				fmt.Printf("ct ws gc: %sremove %s (%s)\n", act, key, status)
				if !dry {
					if err := wsRm(ctx, key, false); err != nil {
						warnf("%v", err)
						warnf("gc: kept %s (see above)", key)
						if info.Vault != "" {
							kept[info.Vault] = append(kept[info.Vault], fmt.Sprintf(
								"Clean up the workspace of [[%s]] (%s): %s has uncommitted or unpushed work. Review it with ct ws diff %s, then ct ws rm %s (--force to drop it)",
								key, status, dir, key, key))
						}
					}
					if !fsx.IsDir(dir) {
						continue
					}
				}
			}
		}

		if !running && fsx.IsDir(filepath.Join(dir, "graphify-out")) {
			fmt.Printf("ct ws gc: %sdrop the merged graph of %s\n", act, key)
			if !dry {
				_ = os.RemoveAll(filepath.Join(dir, "graphify-out"))
			}
		}

		if kb && !running {
			info, _ := workspace.Read(dir)
			for _, r := range info.Repos {
				if r.Mode != "explore" || !fsx.IsDir(filepath.Join(r.Path, ".git")) {
					continue
				}
				if fsx.OlderThanDays(filepath.Join(r.Path, ".git", "HEAD"), days) && !gitx.Dirty(r.Path) {
					fmt.Printf("ct ws gc: %sremove kb exploration clone %s\n", act, r.Slug)
					if !dry {
						_ = os.RemoveAll(r.Path)
						if err := workspace.Drop(ws, r.Slug); err != nil {
							warnf("gc: couldn't update %s: %v", ws, err)
						}
					}
				}
			}
		}
	}

	if !dry {
		outs := graphOutDirs(ctx)
		graph.PruneSnapshots(outs...)
		fmt.Printf("ct ws gc: pruned graphify snapshots older than a week in %d graph folder(s)\n", len(outs))
	}

	vaults := make([]string, 0, len(kept))
	for v := range kept {
		vaults = append(vaults, v)
	}
	sort.Strings(vaults)
	for _, v := range vaults {
		n, err := followup.Write(v, "ct-ws-gc", "ct ws gc", kept[v])
		if err != nil {
			warnf("gc: couldn't write the follow-up note in %s: %v", v, err)
		} else if n != "" {
			fmt.Printf("ct ws gc: follow-ups in %s\n", n)
		}
	}
	return nil
}

// graphOutDirs is every graphify-out of the main clones and of the
// vault's ticket and kb checkouts.
func graphOutDirs(ctx vault.Context) []string {
	var out []string
	main, _ := filepath.Glob(filepath.Join(ctx.ProjectsRoot, "*", "*", "*", "graphify-out"))
	ws, _ := filepath.Glob(filepath.Join(ctx.WorkRoot, "*", "*", "graphify-out"))
	for _, d := range append(main, ws...) {
		rel, _ := filepath.Rel(ctx.WorkRoot, d)
		if top := strings.Split(rel, string(filepath.Separator))[0]; strings.HasPrefix(d, ctx.WorkRoot+string(filepath.Separator)) &&
			strings.HasPrefix(top, ".") && !strings.HasPrefix(top, ".kb-") {
			continue
		}
		if fsx.IsDir(d) {
			out = append(out, d)
		}
	}
	return out
}

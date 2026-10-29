package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/followup"
	"github.com/zero4573/claude-tickets/internal/fsx"
	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/graph"
	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/repo"
	"github.com/zero4573/claude-tickets/internal/session"
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
<workRoot>/<ID>/<slug>, on branch feature/<ID>[-<description>] (a manual ticket's: feature/<description>). Each
workspace's repos are recorded in <workRoot>/<ID>/workspace.json.

A <repo> argument is a slug or <provider>/<owner>/<repo>.`,
	}
	cmd.AddCommand(wsReposCmd(), wsCloneCmd(), wsAddCmd(), wsLsCmd(), wsDiffCmd(),
		wsDiffCheckCmd(), wsRmCmd(), wsMarkCmd(), wsMarksCmd(), wsFetchCmd(), wsGcCmd(),
		wsTidyCleanCmd(), wsRefreshCmd())
	return cmd
}

func warnf(format string, a ...any) { fmt.Fprintf(os.Stderr, "ct: "+format+"\n", a...) }

func completeRepos(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	ctx, _ := vault.Optional()
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
			ctx, err := vault.Optional()
			if err != nil {
				return err
			}
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
code graph; a repo already cloned there is left as it is (never cloned
twice). The default URL is SSH (git@bitbucket.org:<owner>/<repo>.git, with
CLAUDE_TICKETS_BITBUCKET_HOST for another Bitbucket host, or the GitHub
equivalent). Sessions run it too, then ct ws add makes the ticket's
worktree from the new clone.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Optional()
			if err != nil {
				return err
			}
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
			dest, err := repo.CloneDest(ctx.ProjectsRoot, id)
			if err != nil {
				return fmt.Errorf("clone: %s: %w", url, err)
			}
			if fsx.IsDir(filepath.Join(dest, ".git")) {
				fmt.Printf("ct ws: %s is already cloned at %s\n", clone, dest)
				return nil
			}
			if _, err := os.Lstat(dest); err == nil {
				return fmt.Errorf("clone: %s exists and is not a clone; move it away first", dest)
			}
			// MkdirAll and git clone follow symlinks, which a session can
			// plant in the projects root: check what exists before creating
			// anything, check the folders again, and clone into a temporary
			// name beside dest, renamed into place once the parent checks again
			if err := repo.CheckCloneAncestors(ctx.ProjectsRoot, dest); err != nil {
				return fmt.Errorf("clone: %w", err)
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			if err := repo.CheckCloneParent(ctx.ProjectsRoot, dest); err != nil {
				return fmt.Errorf("clone: %w", err)
			}
			tmp := filepath.Join(filepath.Dir(dest), ".ct-clone-"+filepath.Base(dest)+"-"+strconv.Itoa(os.Getpid()))
			if err := fsx.RemoveAll(tmp); err != nil {
				return err
			}
			if gitx.Clone(url, tmp, os.Stdin, os.Stdout, os.Stderr) != nil {
				// tmp was absent before this run, so whatever is there is ours
				_ = fsx.RemoveAll(tmp)
				return errors.New("clone: git clone failed (no git credentials here? run it on the host)")
			}
			if err := repo.CheckCloneParent(ctx.ProjectsRoot, dest); err != nil {
				return fmt.Errorf("clone: %w (the clone is left at %s)", err, tmp)
			}
			if _, err := os.Lstat(dest); err == nil {
				_ = fsx.RemoveAll(tmp)
				return fmt.Errorf("clone: %s appeared during the clone; left alone", dest)
			}
			if err := os.Rename(tmp, dest); err != nil {
				_ = fsx.RemoveAll(tmp)
				return err
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
main clone's .worktreeinclude (gitignore syntax, e.g. .env) and seeds the
worktree's code graph from the main clone's graphify-out/ so it builds
incrementally. The worktree's graph lives outside it, in
<workRoot>/<ID>/.cache/graphify/<slug>/graphify-out, so nothing graphify
writes lands in the checkout.
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
	cmd.Flags().StringVar(&desc, "desc", "", "description for the branch name (feature/<ID>-<description>; feature/<description> for a manual ticket)")
	cmd.Flags().StringVar(&version, "version", "", "the release the change targets (recorded in workspace.json)")
	cmd.Flags().StringVar(&useBranch, "branch", "", "check out this existing branch instead")
	return cmd
}

// branchTakenBy names another workspace whose workspace.json records branch
// for repo slug ("" when none does). A manual ticket's branch carries no key,
// so two tickets with the same --desc would otherwise share it silently.
func branchTakenBy(workRoot, key, slug, branch string) string {
	for _, dir := range workspace.List(workRoot, false) {
		if filepath.Base(dir) == key {
			continue
		}
		info, err := workspace.Read(dir)
		if err != nil {
			continue
		}
		for _, r := range info.Repos {
			if r.Slug == slug && r.Branch == branch {
				return filepath.Base(dir)
			}
		}
	}
	return ""
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
		return fmt.Errorf("add: no main clone '%s' (see ct ws repos, or clone it: ct ws clone <provider>/<owner>/<repo>)", spec)
	}
	slug := repo.Slug(clone)
	main := filepath.Join(ctx.ProjectsRoot, clone)
	dir := filepath.Join(ctx.WorkRoot, key)
	wt := filepath.Join(dir, slug)
	_, statErr := os.Stat(filepath.Join(wt, ".git"))
	synced := note.Synced(note.Frontmatter(note.TicketPath(ctx.Vault, key)))
	var branch string
	switch {
	case useBranch != "":
		branch = useBranch
	case synced:
		branch = "feature/" + key
		if desc != "" {
			branch += "-" + repo.Kebab(desc)
		}
	case desc != "":
		branch = "feature/" + repo.Kebab(desc)
	case statErr == nil:
		// the checkout's own branch is used below
	default:
		return errors.New("add: a manual ticket's branch needs --desc (or --branch)")
	}
	if !synced && useBranch == "" && desc != "" && statErr != nil {
		if other := branchTakenBy(ctx.WorkRoot, key, slug, branch); other != "" {
			return fmt.Errorf("add: branch '%s' is already used by %s in %s; pick another --desc, or pass --branch %s to share it", branch, other, slug, branch)
		}
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
			found, _ := gitx.Out(main, "for-each-ref", "--format=%(refname:short)",
				"refs/heads/feature/"+key, "refs/heads/feature/"+key+"-*")
			if first, _, _ := strings.Cut(found, "\n"); first != "" {
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

		// Always a worktree of the one main clone, never a second copy of the
		// repo. CLAUDE_TICKETS_WS_SHARED_CLONE=1 takes its .git as read-only
		// (testing; older workspaces may still hold shared clones, mode "clone")
		if !gitx.Writable(filepath.Join(main, ".git", "refs")) || os.Getenv("CLAUDE_TICKETS_WS_SHARED_CLONE") != "" {
			return fmt.Errorf("add: %s isn't writable here, so no worktree can be made; run ct ws add on the host, or restart the session (ct start %s)",
				config.TildePath(filepath.Join(main, ".git")), key)
		}
		mode = "worktree"
		if from == "local" {
			err = gitx.Run(main, "worktree", "add", "--quiet", wt, branch)
		} else {
			err = gitx.Run(main, "worktree", "add", "--quiet", "--no-track", "-b", branch, wt, commit)
		}
		if err != nil {
			return fmt.Errorf("add: git worktree add failed in %s", slug)
		}
		if from == "new" {
			fmt.Printf("ct ws: created %s (%s) on %s (from %s)\n", wt, mode, branch, start)
		} else {
			fmt.Printf("ct ws: created %s (%s) on existing branch %s\n", wt, mode, branch)
		}
		copyWorktreeIncludes(main, wt)
		graph.Seed(main, graph.WorktreeOut(dir, slug))
	}
	parts := strings.SplitN(clone, "/", 3)
	if err := workspace.Put(workspace.File(dir), workspace.Repo{
		Provider: parts[0], Owner: parts[1], Repo: parts[2], Slug: slug,
		Path: wt, Base: base, Branch: branch, Mode: mode, TargetVersion: &version,
	}); err != nil {
		return err
	}
	writeToolVersions(ctx, dir)
	return nil
}

// writeToolVersions writes the workspace's .tool-versions for the repos in
// its workspace.json (slug to path); a missing tool or a failed write is a
// warning, not an error.
func writeToolVersions(ctx vault.Context, dir string) {
	repos := map[string]string{}
	if info, err := workspace.Read(dir); err == nil {
		for _, r := range info.Repos {
			repos[r.Slug] = r.Path
		}
	}
	if err := session.WriteToolVersions(dir, ctx.Vault, repos, func(m string) { fmt.Fprintln(os.Stderr, m) }); err != nil {
		warnf("couldn't write %s: %v", filepath.Join(dir, ".tool-versions"), err)
	}
}

// copyWorktreeIncludes copies the untracked, ignored files a checkout
// needs (.env, local config) that match the main clone's .worktreeinclude.
func copyWorktreeIncludes(main, wt string) {
	if _, err := os.Stat(filepath.Join(main, ".worktreeinclude")); err != nil {
		return
	}
	for _, f := range worktreeIncludeFiles(main) {
		if err := fsx.CopyFile(filepath.Join(main, f), filepath.Join(wt, f)); err != nil {
			warnf("couldn't copy %s into %s: %v", f, wt, err)
		}
	}
}

// worktreeIncludeFiles lists the main clone's ignored files that match its
// .worktreeinclude.
func worktreeIncludeFiles(main string) []string {
	if _, err := os.Stat(filepath.Join(main, ".worktreeinclude")); err != nil {
		return nil
	}
	out, err := gitx.Out(main, "ls-files", "-z", "--others", "--ignored", "--exclude-from=.worktreeinclude")
	if err != nil {
		return nil
	}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

// withoutSeeded drops from names the files ct ws add seeded from the main
// clone's .worktreeinclude that still match the main clone's copy byte for
// byte: they are recreated by the next add, so they don't count as the
// user's.
func withoutSeeded(names []string, main, wt string) []string {
	seeded := map[string]bool{}
	for _, f := range worktreeIncludeFiles(main) {
		seeded[f] = true
	}
	var keep []string
	for _, n := range names {
		if !seeded[n] || !sameRegularFile(filepath.Join(main, n), filepath.Join(wt, n)) {
			keep = append(keep, n)
		}
	}
	return keep
}

// sameRegularFile reports whether a and b are regular files with the same
// bytes. It opens each without following a symlink or blocking on a FIFO
// (a worktree is writable by sandboxed agents, so a swapped-in special file
// must not hang ct), checks the opened file and compares in chunks.
func sameRegularFile(a, b string) bool {
	fa, err := openRegular(a)
	if err != nil {
		return false
	}
	defer fa.Close()
	fb, err := openRegular(b)
	if err != nil {
		return false
	}
	defer fb.Close()
	sa, err := fa.Stat()
	if err != nil || !sa.Mode().IsRegular() {
		return false
	}
	sb, err := fb.Stat()
	if err != nil || !sb.Mode().IsRegular() || sa.Size() != sb.Size() {
		return false
	}
	ba, bb := make([]byte, 32<<10), make([]byte, 32<<10)
	for {
		na, ea := io.ReadFull(fa, ba)
		nb, eb := io.ReadFull(fb, bb)
		if na != nb || !bytes.Equal(ba[:na], bb[:nb]) {
			return false
		}
		if ea != nil || eb != nil {
			// both ended together (EOF or short read) or one failed
			return ea == eb || (isEOF(ea) && isEOF(eb))
		}
	}
}

func isEOF(err error) bool { return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) }

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
	var drop, force bool
	cmd := &cobra.Command{
		Use:   "rm <ID> [--drop-ignored]",
		Short: "Remove a ticket's worktrees and workspace files (branches are kept)",
		Long: `Removes the ticket's worktrees and its workspace files. Branches are kept.

Every guard is checked and a refusal lists all that apply, with the items
behind each: uncommitted changes (modified and untracked files), a submodule
checked out, ignored files (.env, local configs) that removal would delete, and,
for an older workspace's shared clone, commits that aren't on origin.

--drop-ignored lifts only the ignored-files stop. Nothing skips the other
guards: commit, push or stash the work, or remove the directory by hand.
--force is a deprecated alias of --drop-ignored.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeWorkspaceID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			return wsRemoveOpts(ctx, args[0], drop || force, false, true)
		},
	}
	cmd.Flags().BoolVar(&drop, "drop-ignored", false, "remove the ignored files (.env, local configs) with the tree")
	cmd.Flags().BoolVar(&force, "force", false, "deprecated alias of --drop-ignored")
	_ = cmd.Flags().MarkHidden("force")
	return cmd
}

func wsRm(ctx vault.Context, key string, dropIgnored bool) error {
	return wsRemove(ctx, key, dropIgnored, false)
}

// wsRemove is wsRm; merged also lets a shared clone go when its unpushed
// commits are already in origin/<base> (gitx.Lost: e.g. squash-merged, the
// branch deleted on the server), as ct clean judges it. keepIgnored (ct ws
// rm) also keeps a tree holding ignored files of the user's (.env, local
// configs) unless dropIgnored; without keepIgnored (gc, ct clean) those
// files are moved to the vault's .trash/ first.
func wsRemove(ctx vault.Context, key string, dropIgnored, merged bool) error {
	return wsRemoveOpts(ctx, key, dropIgnored, merged, false)
}

func wsRemoveOpts(ctx vault.Context, key string, dropIgnored, merged, keepIgnored bool) error {
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
			// git's own check (a plain `git worktree remove`) runs status in
			// each submodule, so ct decides with its guarded status, which
			// doesn't enter them, and keeps any tree with a submodule checked
			// out: its changes and commits (its git dir is under this git
			// dir's modules/) would go with it. Every guard is evaluated, so the
			// refusal names all that apply, with their items.
			var reasons []string
			if modified, untracked, err := dirtyItems(r.Path); err != nil {
				reasons = append(reasons, "uncommitted changes git can't list ("+err.Error()+")")
			} else {
				if len(modified) > 0 {
					reasons = append(reasons, "modified files: "+listNames(modified)+" (commit or stash them)")
				}
				if len(untracked) > 0 {
					reasons = append(reasons, "untracked files: "+listNames(untracked)+" (commit or stash them)")
				}
			}
			if sub := gitx.PopulatedSubmodule(r.Path); sub == "?" {
				reasons = append(reasons, "submodules git can't list")
			} else if sub != "" {
				reasons = append(reasons, fmt.Sprintf("a submodule checked out at %s, whose work ct doesn't check (check it by hand)", sub))
			}
			names := withoutSeeded(ignoredFiles(r.Path), main, r.Path)
			repos := nestedRepos(r.Path, names)
			if len(repos) > 0 {
				names = withoutNames(names, repos)
				if keepIgnored && !dropIgnored {
					var items []string
					for _, n := range repos {
						items = append(items, n+" (unpushed: "+unpushedItems(filepath.Join(r.Path, n))+")")
					}
					reasons = append(reasons, "ignored folders that hold a git repository: "+strings.Join(items, "; ")+" (move them out, or use --drop-ignored to move them to the trash)")
				}
			}
			if keepIgnored && !dropIgnored && len(names) > 0 {
				reasons = append(reasons, "ignored files that removing it would delete: "+listNames(names)+" (keep them, or use --drop-ignored)")
			}
			if st.IsDir() {
				// A shared clone (see add): its branches live only in it, so refuse
				// to drop work that was never pushed (or that git can't vouch for)
				unpushed, err := gitx.Unpushed(r.Path)
				if merged && (err != nil || unpushed) {
					unpushed, err = gitx.Lost(r.Path, r.Base)
				}
				if err != nil {
					reasons = append(reasons, "commits or stashes git can't vouch for ("+err.Error()+")")
				} else if unpushed {
					reasons = append(reasons, "commits that aren't on origin or stashed changes: "+unpushedItems(r.Path)+" (push them)")
				}
			}
			if len(reasons) > 0 {
				warnf("%s not removed:\n  - %s", r.Path, strings.Join(reasons, "\n  - "))
				failed = true
				continue
			}
			ignored := ""
			trash := filepath.Join(ctx.Vault, ".trash", key+"-ignored-files", r.Slug)
			copied := ""
			if len(names) > 0 && !keepIgnored && !dropIgnored {
				// gc and ct clean never delete the user's ignored files (.env
				// with local secrets) outright: they go to the vault's .trash/.
				// They are copied before any nested repository moves, so a
				// failed copy leaves the worktree as it was.
				moved, dropped, err := trashFiles(r.Path, trash, names)
				if err != nil {
					warnf("%s not removed: couldn't move its ignored files to %s: %v", r.Path, trash, err)
					failed = true
					continue
				}
				ignored += "; ignored files moved to " + config.TildePath(trash) + ": " + listNames(moved)
				if len(dropped) > 0 {
					ignored += "; ignored files removed without a copy (build output, bulk or large): " + listNames(dropped)
				}
				if len(moved) > 0 {
					copied = "; its ignored files were already copied to " + config.TildePath(trash)
				}
			} else if len(names) > 0 {
				ignored += "; ignored files removed with it: " + listNames(names)
			}
			if len(repos) > 0 {
				keep, drop := splitDependencyRepos(r.Path, repos)
				var movedRepos []string
				var moveErr error
				for _, n := range keep {
					dest := uniqueDest(filepath.Join(trash, filepath.Clean(n)))
					if moveErr = moveTree(r.Path, n, dest); moveErr != nil {
						moveErr = fmt.Errorf("%s: %w", n, moveErr)
						break
					}
					movedRepos = append(movedRepos, n+" -> "+config.TildePath(dest))
				}
				if moveErr != nil {
					done := copied
					if len(movedRepos) > 0 {
						done += "; nested repositories already moved to the trash: " + strings.Join(movedRepos, ", ")
					}
					warnf("%s not removed: couldn't move its nested repository to %s: %v%s", r.Path, trash, moveErr, done)
					failed = true
					continue
				}
				if len(movedRepos) > 0 {
					ignored += "; nested repositories moved to the trash: " + strings.Join(movedRepos, ", ")
				}
				if len(drop) > 0 {
					ignored += "; nested repositories removed with it (clean clones under a build or dependency folder): " + strings.Join(drop, ", ")
				}
			}
			if st.IsDir() {
				if err := os.RemoveAll(r.Path); err != nil {
					return err
				}
				fmt.Printf("ct ws: removed %s (shared clone)%s\n", r.Path, ignored)
				continue
			}
			// a legacy in-worktree graphify-out is ignored, so `git worktree remove` would refuse it
			_ = os.RemoveAll(filepath.Join(r.Path, "graphify-out"))
			// checked above, so always --force (gitx refuses it without)
			if err := gitx.Run(main, "worktree", "remove", "--force", r.Path); err != nil {
				warnf("couldn't remove the worktree %s", r.Path)
				failed = true
				continue
			}
			fmt.Printf("ct ws: removed %s (branch %s kept)%s\n", r.Path, r.Branch, ignored)
		}
		_ = gitx.Timeout(time.Minute, main, "worktree", "prune")
	}
	if failed {
		return fmt.Errorf("rm: some worktrees were kept; workspace %s left in place", dir)
	}
	for _, f := range []string{"graphify-out", ".claude", "workspace.json", "workspace.json.lock",
		"CLAUDE.md", ".ignore", ".tool-versions", ".cache", ".agent-state", ".sessions.json", ".sessions.json.lock", key + ".code-workspace"} {
		_ = fsx.RemoveAll(filepath.Join(dir, f))
	}
	fsx.RemoveEmptyDirs(dir)
	if _, err := os.Stat(dir); err == nil {
		warnf("left %s in place: it still has other files", dir)
	} else {
		fmt.Printf("ct ws: removed workspace %s\n", dir)
	}
	return nil
}

// Limits of what gc and ct clean copy into the vault's .trash/: a few small
// user files (.env, local configs), never build output or dependency trees.
const (
	trashMaxFile  = 1 << 20  // one file
	trashMaxTotal = 20 << 20 // all files of one worktree
	trashMaxCount = 200      // files of one worktree
	trashMaxDir   = 50       // files under one top-level folder before it counts as bulk
)

// bulkDirs are folder names that hold generated or downloaded files.
var bulkDirs = map[string]bool{
	"node_modules": true, "target": true, "dist": true, "build": true, "bin": true, "obj": true,
	"out": true, "vendor": true, ".venv": true, "venv": true, "__pycache__": true, ".cache": true,
	".gradle": true, ".next": true, ".nuxt": true, ".tox": true, ".terraform": true,
}

// bulkPath reports whether the slash path n lies under a build-output folder.
func bulkPath(n string) bool {
	parts := strings.Split(filepath.ToSlash(n), "/")
	for _, p := range parts[:len(parts)-1] {
		if bulkDirs[p] {
			return true
		}
	}
	return false
}

// trashFiles copies the files names (relative to dir) under trash, keeping
// their folders, and returns the ones it copied and the ones it left out
// (build output, folders with many files, files over the size or count caps,
// and anything that isn't a regular file: a symlink or special file holds no
// data of the user's). Each file is checked with Lstat, opened under the root
// without blocking, and copied from the opened handle once fstat says it is
// regular; the open never leaves dir. Every name
// resolves under an os.Root of dir, so neither a symlink nor a parent folder
// swapped for one can reach a file outside the worktree. Any copy failure
// stops before anything is removed.
func trashFiles(dir, trash string, names []string) (moved, dropped []string, err error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	perTop := map[string]int{}
	for _, n := range names {
		if top, _, ok := strings.Cut(filepath.ToSlash(n), "/"); ok {
			perTop[top]++
		}
	}
	var total int64
	for _, n := range names {
		if strings.HasSuffix(n, "/") || strings.HasSuffix(n, string(filepath.Separator)) {
			dropped = append(dropped, n)
			continue
		}
		top, _, nested := strings.Cut(filepath.ToSlash(n), "/")
		if bulkPath(n) || (nested && perTop[top] > trashMaxDir) || len(moved) >= trashMaxCount {
			dropped = append(dropped, n)
			continue
		}
		if lst, err := root.Lstat(n); err != nil || !lst.Mode().IsRegular() {
			dropped = append(dropped, n)
			continue
		}
		f, err := openRegularIn(root, n)
		if err != nil {
			dropped = append(dropped, n)
			continue
		}
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() || st.Size() > trashMaxFile || total+st.Size() > trashMaxTotal {
			f.Close()
			dropped = append(dropped, n)
			continue
		}
		err = copyHandle(f, st, filepath.Join(trash, n))
		f.Close()
		if err != nil {
			return moved, dropped, err
		}
		total += st.Size()
		moved = append(moved, n)
	}
	return moved, dropped, nil
}

// copyHandle writes the open file in (already checked as regular) to dest,
// keeping its mode and time.
func copyHandle(in *os.File, st os.FileInfo, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(in, trashMaxFile+1)); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Chmod(dest, st.Mode().Perm())
	return os.Chtimes(dest, st.ModTime(), st.ModTime())
}

// nestedRepos names the ignored folders in names that hold a git repository
// of their own (git lists such a folder as one entry, dir/).
func nestedRepos(dir string, names []string) []string {
	var repos []string
	for _, n := range names {
		if strings.HasSuffix(n, "/") {
			if _, err := os.Lstat(filepath.Join(dir, n, ".git")); err == nil {
				repos = append(repos, n)
			}
		}
	}
	return repos
}

// withoutNames returns names less the entries in drop.
func withoutNames(names, drop []string) []string {
	skip := map[string]bool{}
	for _, d := range drop {
		skip[d] = true
	}
	var out []string
	for _, n := range names {
		if !skip[n] {
			out = append(out, n)
		}
	}
	return out
}

// uniqueDest returns dest, or dest with a numeric suffix when it exists.
func uniqueDest(dest string) string {
	if _, err := os.Lstat(dest); err != nil {
		return dest
	}
	for i := 1; ; i++ {
		d := fmt.Sprintf("%s.%d", dest, i)
		if _, err := os.Lstat(d); err != nil {
			return d
		}
	}
}

// splitDependencyRepos splits the nested repositories repos (folders of dir)
// into the ones to keep in the trash and the clean clones under a build or
// dependency folder (node_modules/<pkg>, .venv/src/<pkg>), whose history a
// remote holds and which go with the worktree. A repository git can't vouch
// for is kept.
func splitDependencyRepos(dir string, repos []string) (keep, drop []string) {
	for _, n := range repos {
		if bulkPath(n) && cleanRepo(filepath.Join(dir, n)) {
			drop = append(drop, n)
		} else {
			keep = append(keep, n)
		}
	}
	return keep, drop
}

// cleanRepo reports whether the repository at dir has no commit missing from
// its remotes (on HEAD, detached or not, or on any branch), no stash, no
// changed or untracked file, and no ignored file of its own outside a build
// or dependency folder (a .env with local secrets).
func cleanRepo(dir string) bool {
	if unpushed, err := gitx.Unpushed(dir); err != nil || unpushed {
		return false
	}
	modified, untracked, err := dirtyItems(dir)
	if err != nil || len(modified) > 0 || len(untracked) > 0 {
		return false
	}
	out, err := gitx.Out(dir, "--no-optional-locks", "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory")
	if err != nil {
		return false
	}
	for _, n := range strings.Split(out, "\x00") {
		if n != "" && !bulkPath(n) {
			return false
		}
	}
	return true
}

// beforeRename runs between moveTree's checks and its rename (a test seam
// that swaps a parent folder in that gap).
var beforeRename = func() {}

// moveTree moves the folder n of dir to dest whole: a rename when both are
// on one filesystem, else a copy (symlinks recreated, never followed) and
// then the removal of n. A failed copy removes the partial copy and keeps n.
// It first checks that no parent of n resolves outside dir and that n itself
// is a folder, not a symlink. The rename then opens n's parent through an
// os.Root of dir (renameIn), so a parent swapped for a symlink after the
// check can't lead it outside dir; the copy reads through the same root,
// which keeps every open inside n.
func moveTree(dir, n, dest string) error {
	n = filepath.Clean(n)
	if !filepath.IsLocal(n) {
		return fmt.Errorf("%s is not a path inside %s", n, dir)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	src := filepath.Join(dir, n)
	if err := checkInside(dir, n); err != nil {
		return err
	}
	beforeRename()
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := renameIn(root, src, n, dest); err == nil {
		return nil
	}
	sub, err := root.OpenRoot(n)
	if err != nil {
		return err
	}
	err = copyTree(sub, dest)
	sub.Close()
	if err != nil {
		_ = os.RemoveAll(dest)
		return err
	}
	return root.RemoveAll(n)
}

// checkInside fails unless the parent folders of n resolve to the same
// folders under dir (none was swapped for a symlink) and n is a folder.
func checkInside(dir, n string) error {
	base, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Join(dir, filepath.Dir(n)))
	if err != nil {
		return err
	}
	if parent != filepath.Join(base, filepath.Dir(n)) {
		return fmt.Errorf("%s: a parent folder leads outside %s", n, dir)
	}
	st, err := os.Lstat(filepath.Join(dir, n))
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a folder", n)
	}
	return nil
}

// copyTree copies the tree under src to dest: folders, symlinks as links
// (never followed) and regular files, each opened under src without blocking
// and copied only once its handle is checked as regular. Anything else (a
// FIFO, a device, a file swapped for one) fails the copy.
func copyTree(src *os.Root, dest string) error {
	return fs.WalkDir(src.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		t := filepath.Join(dest, filepath.FromSlash(p))
		st, err := src.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case st.Mode()&os.ModeSymlink != 0:
			l, err := src.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(l, t)
		case st.IsDir():
			return os.MkdirAll(t, st.Mode().Perm()|0o700)
		case st.Mode().IsRegular():
			in, err := openRegularIn(src, p)
			if err != nil {
				return err
			}
			defer in.Close()
			fst, err := in.Stat()
			if err != nil {
				return err
			}
			if !fst.Mode().IsRegular() {
				return fmt.Errorf("%s is no longer a regular file", p)
			}
			out, err := os.OpenFile(t, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fst.Mode().Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
		return fmt.Errorf("%s is not a regular file, folder or symlink", p)
	})
}

// ignoredFiles names the ignored files (.env, local configs) of the work
// tree at dir one by one (never a collapsed folder), but not ct's own
// graphify-out; none when git can't tell.
func ignoredFiles(dir string) []string {
	out, err := gitx.Out(dir, "--no-optional-locks", "ls-files", "-z", "--others", "--ignored", "--exclude-standard")
	if err != nil {
		return nil
	}
	var names []string
	for _, n := range strings.Split(out, "\x00") {
		if n != "" && !strings.HasPrefix(n, "graphify-out/") {
			names = append(names, n)
		}
	}
	return names
}

// dirtyItems lists the tracked files with changes and the untracked ones
// (as git status shows them).
func dirtyItems(dir string) (modified, untracked []string, err error) {
	out, err := gitx.Out(dir, "--no-optional-locks", "status", "--porcelain", "-z", "--untracked-files=normal")
	if err != nil {
		return nil, nil, err
	}
	ents := strings.Split(out, "\x00")
	for i := 0; i < len(ents); i++ {
		e := ents[i]
		if len(e) < 4 {
			continue
		}
		if strings.ContainsAny(e[:2], "RC") {
			i++ // the rename's origin follows as its own entry
		}
		if e[:2] == "??" {
			untracked = append(untracked, e[3:])
		} else {
			modified = append(modified, e[3:])
		}
	}
	return modified, untracked, nil
}

// unpushedItems names what a clone holds that no remote has: each commit
// (sha and subject) and the stash entries.
func unpushedItems(dir string) string {
	var items []string
	if out, err := gitx.Out(dir, "log", "--branches", "--not", "--remotes", "--format=%h %s"); err == nil {
		for _, l := range strings.Split(out, "\n") {
			if l != "" {
				items = append(items, l)
			}
		}
	}
	if out, err := gitx.Out(dir, "stash", "list"); err == nil && out != "" {
		items = append(items, fmt.Sprintf("%d stash entries", strings.Count(out, "\n")+1))
	}
	if len(items) == 0 {
		return "(none listed)"
	}
	return listNames(items)
}

// listNames joins names on one line, the first ten.
func listNames(names []string) string {
	const max = 10
	more := ""
	if len(names) > max {
		more = fmt.Sprintf(" and %d more", len(names)-max)
		names = names[:max]
	}
	return strings.Join(names, ", ") + more
}

func wsFetchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fetch [<repo>...]",
		Short: "Fetch origin in the main clones and pass the refs on to shared clones",
		Long: `Fetches origin in the given main clones (default: all), in parallel, then
passes the new refs on to the shared clones in the vault's ticket and spec
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
	// Shared clones (ticket and spec workspaces) copy their refs from the main
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
		Short: "Free disk: finished workspaces, idle merged graphs and spec clones, old snapshots",
		Long: `Frees disk in the current vault: removes workspaces of tickets that are
done or closed and idle for N days (default 14; dirty or unpushed work is
kept, and left as a follow-up task in the vault's inbox), merged graphs of
sessions that aren't running, idle spec exploration clones, and graphify's
dated snapshots older than a week. A stopped session's .cache goes too,
worktree graphs included: the next start re-seeds each from its main
clone's graph and updates it.

A session counts as running when its process (ct start, ct teach, ct spec)
is alive on this host, else, when that can't be checked, when its hook
state (.agent-state) is less than a day old and not exited.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if days < 0 {
				return errors.New("gc: --days must be a number")
			}
			// It tells running sessions apart by the sessions' PIDs; inside a
			// container it would see none and clean up live workspaces
			if gitx.InContainer() {
				return errors.New("gc: run it on the host, not inside a container")
			}
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			return wsGc(ctx, days, dry)
		},
	}
	cmd.Flags().IntVar(&days, "days", 14, "idle days before a finished workspace or spec clone goes")
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "only say what would go")
	return cmd
}

func wsGc(ctx vault.Context, days int, dry bool) error {
	act := ""
	if dry {
		act = "would "
	}
	// Workspaces kept although their ticket is finished, per vault: tasks
	// for a follow-up note in that vault's inbox/
	kept := map[string][]string{}
	for _, dir := range workspace.List(ctx.WorkRoot, true) {
		key := filepath.Base(dir)
		spec := strings.HasPrefix(key, ".spec-")
		ws := workspace.File(dir)
		running := workspace.Running(dir)

		if !spec && !running && fsx.OlderThanDays(ws, days) {
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
								"Clean up the workspace of [[%s]] (%s): %s has uncommitted or unpushed work. Review it with ct ws diff %s, then ct ws rm %s (--drop-ignored for ignored files only)",
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
		// Go's module and build caches (session.GoEnv) refill when next used
		if cache := session.CacheDir(dir); !running && fsx.IsDir(cache) {
			fmt.Printf("ct ws gc: %sdrop the caches of %s\n", act, key)
			if !dry {
				_ = fsx.RemoveAll(cache)
			}
		}

		if spec && !running {
			info, _ := workspace.Read(dir)
			for _, r := range info.Repos {
				if r.Mode != "explore" || !fsx.IsDir(filepath.Join(r.Path, ".git")) {
					continue
				}
				if fsx.OlderThanDays(filepath.Join(r.Path, ".git", "HEAD"), days) && !gitx.Dirty(r.Path) && gitx.PopulatedSubmodule(r.Path) == "" {
					fmt.Printf("ct ws gc: %sremove spec exploration clone %s\n", act, r.Slug)
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

// graphOutDirs is every graphify-out of the main clones, of the vault's spec
// checkouts and legacy ticket worktrees, and the ticket worktrees' graphs
// under <WorkRoot>/<ID>/.cache/graphify/<slug>/graphify-out.
func graphOutDirs(ctx vault.Context) []string {
	var out []string
	main, _ := filepath.Glob(filepath.Join(ctx.ProjectsRoot, "*", "*", "*", "graphify-out"))
	ws, _ := filepath.Glob(filepath.Join(ctx.WorkRoot, "*", "*", "graphify-out"))
	cached, _ := filepath.Glob(filepath.Join(ctx.WorkRoot, "*", ".cache", "graphify", "*", "graphify-out"))
	for _, d := range append(append(main, ws...), cached...) {
		rel, _ := filepath.Rel(ctx.WorkRoot, d)
		if top := strings.Split(rel, string(filepath.Separator))[0]; strings.HasPrefix(d, ctx.WorkRoot+string(filepath.Separator)) &&
			strings.HasPrefix(top, ".") && !strings.HasPrefix(top, ".spec-") {
			continue
		}
		if fsx.IsDir(d) {
			out = append(out, d)
		}
	}
	return out
}

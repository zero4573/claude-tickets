package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/fsx"
	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/repo"
	"github.com/zero4573/claude-tickets/internal/vault"
)

func layoutCmd() *cobra.Command {
	var apply bool
	var root string
	cmd := &cobra.Command{
		Use:   "layout [--apply] [--root <dir>]",
		Short: "Move repos into <projectsRoot>/<provider>/<owner>/<repo>",
		Long: `Reorganizes the git repos under the current vault's projectsRoot
(~/Projects/repo-<vault> by default; see ct vault) or --root, wherever they
are nested, into <root>/<provider>/<owner>/<repo>, the layout the ticket workflow
expects. All three come from each repo's origin URL:
  bitbucket  Server/DC (/scm/<key>/<repo>, ssh :7999/<key>/<repo>; owner =
             project key, upper-cased) or Cloud (owner = workspace)
  github     github.com or GitHub Enterprise (owner = user or org)
  gitlab     owner = group, subgroups joined with -
  other      provider = the host name, kebab-cased
The vault and the code graph name each repo by its slug,
<provider>-<owner>-<repo> (ct ws repos lists them). Repos without a hosted
origin are left where they are.

Without --apply it only prints the plan. Moving a repo is a rename on the
same filesystem, so uncommitted work is kept. Repos with linked worktrees
are skipped, since moving them breaks the worktrees' links back to the repo
(fix after a manual move with git worktree repair), and so are repos that
shared clones in ticket or kb workspaces borrow objects from (remove those
first: ct ws rm or gc). Repos are staged in a temporary folder under the
root and then put in place, so nested repos, and repos sitting where a
provider or owner folder must go (e.g. a repo at <root>/bitbucket), are
handled. Empty folders left behind are removed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var ctx vault.Context
			if root == "" {
				var err error
				if ctx, err = vault.Require(); err != nil {
					return err
				}
				root = ctx.ProjectsRoot
			} else {
				ctx = vault.Optional()
			}
			return layout(ctx, root, apply)
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "move the repos (default: only print the plan)")
	cmd.Flags().StringVar(&root, "root", "", "the folder to reorganize (default: the vault's projectsRoot)")
	return cmd
}

// findRepos is every repo under root (a directory with a .git directory,
// so not linked worktrees), 1 to 7 levels down, deepest first so nested
// repos move before the repos containing them.
func findRepos(root string) []string {
	var repos []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		depth := strings.Count(rel, string(filepath.Separator)) + 1
		switch d.Name() {
		case "node_modules", "graphify-out", ".venv", "vendor":
			return filepath.SkipDir
		case ".git":
			if depth >= 2 {
				repos = append(repos, filepath.Dir(p))
			}
			return filepath.SkipDir
		}
		if depth >= 8 {
			return filepath.SkipDir
		}
		return nil
	})
	sort.Slice(repos, func(i, j int) bool {
		di, dj := fsx.Depth(repos[i]), fsx.Depth(repos[j])
		if di != dj {
			return di > dj
		}
		return repos[i] > repos[j]
	})
	return repos
}

// borrowedRepos is the main clones that shared clones in ticket and kb
// workspaces borrow objects from (git alternates): moving one breaks those
// clones. Every vault whose projectsRoot overlaps root counts, not only
// the current one, since vaults may share their main clones.
func borrowedRepos(ctx vault.Context, root string) map[string]bool {
	workRoots := []string{ctx.WorkRoot}
	for _, v := range vault.List() {
		l := vault.LocationsOf(filepath.Join(config.ObsidianRoot(), v))
		if vault.PathsOverlap(l.ProjectsRoot, root) {
			workRoots = append(workRoots, l.WorkRoot)
		}
	}
	borrowed := map[string]bool{}
	seen := map[string]bool{}
	for _, w := range workRoots {
		if w == "" || !fsx.IsDir(w) {
			continue
		}
		_ = filepath.WalkDir(w, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(w, p)
			if d.IsDir() && rel != "." && strings.Count(rel, string(filepath.Separator)) >= 6 {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(filepath.ToSlash(p), "/.git/objects/info/alternates") || seen[p] {
				return nil
			}
			seen[p] = true
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer f.Close()
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				// git writes these with / on every OS
				if obj := filepath.ToSlash(sc.Text()); strings.HasSuffix(obj, "/.git/objects") {
					borrowed[filepath.FromSlash(strings.TrimSuffix(obj, "/.git/objects"))] = true
				}
			}
			return nil
		})
	}
	return borrowed
}

func layout(ctx vault.Context, root string, apply bool) error {
	if !fsx.IsDir(root) {
		return fmt.Errorf("no such directory: %s", root)
	}
	root, _ = filepath.Abs(root)
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	// Shown with / on every OS: the same as the <provider>/<owner>/<repo> it names
	relOf := func(p string) string {
		if rel, err := filepath.Rel(root, p); err == nil && fsx.Inside(p, root) {
			return filepath.ToSlash(rel)
		}
		return p
	}
	under := fsx.Inside

	repos := findRepos(root)
	borrowed := borrowedRepos(ctx, root)
	borrowedRepo := func(p string) (string, bool) {
		for b := range borrowed {
			if b == p || under(b, p) {
				return b, true
			}
		}
		return "", false
	}

	destOf := map[string]string{}
	claimed := map[string]string{}
	var planned []string
	skipped := 0
	// Repos with linked worktrees: they stay, and so must every repo around
	// them (moving it would carry them along; repos come deepest first)
	var pinned []string
	for _, r := range repos {
		rel := relOf(r)
		url, _ := gitx.Out(r, "remote", "get-url", "origin")
		id, ok := repo.RemoteIdentity(url)
		if !ok {
			if url == "" {
				url = "none"
			}
			fmt.Printf("skip   %s (no hosted origin remote: %s)\n", rel, url)
			skipped++
			continue
		}
		target := id.Clone()
		if wts, _ := gitx.Out(r, "worktree", "list", "--porcelain"); strings.Count("\n"+wts, "\nworktree ") > 1 {
			fmt.Printf("skip   %s -> %s (has linked worktrees; move manually, then git worktree repair)\n", rel, target)
			pinned = append(pinned, r)
			skipped++
			continue
		}
		if p, ok := firstInside(pinned, r); ok {
			fmt.Printf("skip   %s -> %s (holds %s, which has linked worktrees)\n", rel, target, relOf(p))
			pinned = append(pinned, r)
			skipped++
			continue
		}
		if other, ok := claimed[target]; ok {
			fmt.Printf("skip   %s -> %s (another clone of it, %s, goes there)\n", rel, target, other)
			skipped++
			continue
		}
		claimed[target] = rel
		destOf[r] = filepath.Join(root, target)
		planned = append(planned, r)
	}

	// A repo moves if it isn't at its destination yet, or if a repo
	// containing it moves (it would be carried along). A repo can also sit
	// where another repo's provider or owner folder has to go (e.g. a repo at
	// ~/Projects/bitbucket), so every moving repo is first staged outside the
	// tree, deepest first, then put in place.
	isMoving := func(r string) bool {
		if r != destOf[r] {
			return true
		}
		for _, o := range planned {
			if under(r, o) && o != destOf[o] {
				return true
			}
		}
		return false
	}
	// the repo a path is inside of, if that repo isn't moving
	insideStayingRepo := func(p string) (string, bool) {
		for _, o := range repos {
			if _, planned := destOf[o]; under(p, o) && !(planned && isMoving(o)) {
				return o, true
			}
		}
		return "", false
	}
	// true if the path will be moved out of the way
	destVacated := func(p string) bool {
		for _, o := range planned {
			if (p == o || under(p, o)) && isMoving(o) {
				return true
			}
		}
		return false
	}

	var moving []string
	for _, r := range planned {
		rel, dest := relOf(r), destOf[r]
		target := relOf(dest)
		if !isMoving(r) {
			fmt.Printf("ok     %s\n", rel)
			continue
		}
		if r != dest {
			if _, err := os.Lstat(dest); err == nil && !destVacated(dest) {
				fmt.Printf("skip   %s -> %s (destination exists)\n", rel, target)
				skipped++
				continue
			}
			if staying, ok := insideStayingRepo(dest); ok {
				fmt.Printf("skip   %s -> %s (destination is inside the repo %s, which stays)\n", rel, target, relOf(staying))
				skipped++
				continue
			}
			if lender, ok := borrowedRepo(r); ok {
				fmt.Printf("skip   %s -> %s (shared clones in ticket or kb workspaces borrow %s's objects; remove them first with ct ws rm / gc)\n", rel, target, relOf(lender))
				skipped++
				continue
			}
		}
		dirty := ""
		if gitx.Dirty(r) {
			dirty = " (has uncommitted changes, kept)"
		}
		if r == dest {
			fmt.Printf("move   %s (inside a repo that moves; staged and put back)%s\n", rel, dirty)
		} else {
			fmt.Printf("move   %s -> %s%s\n", rel, target, dirty)
		}
		moving = append(moving, r)
	}

	if !apply {
		fmt.Printf("ct layout: %d repo(s) to move, %d skipped (dry run; --apply to move)\n", len(moving), skipped)
		return nil
	}
	if len(moving) == 0 {
		fmt.Println("ct layout: nothing to move")
		return nil
	}

	// Phase 1: stage (planned is deepest first, so nested repos leave first)
	staging, err := os.MkdirTemp(root, ".repo-layout.")
	if err != nil {
		return err
	}
	staged := map[string]string{}
	for i, r := range moving {
		s := filepath.Join(staging, fmt.Sprint(i+1))
		if err := os.Rename(r, s); err != nil {
			return fmt.Errorf("moving %s out of the way: %w (already staged repos are in %s)", relOf(r), err, staging)
		}
		staged[r] = s
		// Remove folders the move emptied, up to the root
		for parent := filepath.Dir(r); under(parent, root) && os.Remove(parent) == nil; parent = filepath.Dir(parent) {
		}
	}

	// Phase 2: put each in place (shallowest destination first)
	order := append([]string{}, moving...)
	sort.SliceStable(order, func(i, j int) bool {
		return fsx.Depth(destOf[order[i]]) < fsx.Depth(destOf[order[j]])
	})
	moved := 0
	for _, r := range order {
		dest := destOf[r]
		if _, err := os.Lstat(dest); err == nil {
			warnf("destination %s exists; left %s at %s", dest, relOf(r), staged[r])
			skipped++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.Rename(staged[r], dest); err != nil {
			return errors.Join(fmt.Errorf("putting %s in place failed; it's at %s", relOf(r), staged[r]), err)
		}
		moved++
	}
	if os.Remove(staging) != nil {
		warnf("some repos are still staged in %s", staging)
	}
	fmt.Printf("ct layout: moved %d repo(s), skipped %d\n", moved, skipped)
	return nil
}

func firstInside(paths []string, dir string) (string, bool) {
	for _, p := range paths {
		if fsx.Inside(p, dir) {
			return p, true
		}
	}
	return "", false
}

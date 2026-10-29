package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/fsx"
	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

func wsTidyCleanCmd() *cobra.Command {
	var force, dry bool
	cmd := &cobra.Command{
		Use:   "tidy-clean <ID> [--force] [--dry-run]",
		Short: "Remove the tidy skill's scratch clones under <ws>/tasks/",
		Long: `Removes the scratch clones the tidy skill leaves in <workRoot>/<ID>/tasks/tidy-<slug>
(the sandbox denies rm -rf, so nobody there can). A clone goes when its
bookmark (the ticket branch) is a commit already on the ticket worktree's
branch; an unmerged one is kept with a warning unless --force. Nothing else
under tasks/ is touched, task worktrees included.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeWorkspaceID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			return wsTidyClean(ctx, args[0], force, dry)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "remove unmerged clones too")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "show what would be removed")
	return cmd
}

func wsTidyClean(ctx vault.Context, key string, force, dry bool) error {
	if err := requireKey("tidy-clean", key); err != nil {
		return err
	}
	dir := filepath.Join(ctx.WorkRoot, key)
	if !fsx.IsDir(dir) {
		return fmt.Errorf("tidy-clean: no workspace at %s", dir)
	}
	tasks := filepath.Join(dir, "tasks")
	entries, _ := os.ReadDir(tasks)
	var names []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "tidy-") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		fmt.Printf("ct ws: no tidy clones in %s\n", tasks)
		return nil
	}
	info, err := workspace.Read(dir)
	if err != nil {
		return fmt.Errorf("tidy-clean: %w", err)
	}
	repos := map[string]workspace.Repo{}
	for _, r := range info.Repos {
		repos[r.Slug] = r
	}
	kept := 0
	for _, name := range names {
		path := filepath.Join(tasks, name)
		slug := strings.TrimPrefix(name, "tidy-")
		reason := tidyUnmerged(repos, slug, path)
		if reason != "" && !force {
			warnf("%s: %s, not removing (use --force)", path, reason)
			kept++
			continue
		}
		if dry {
			fmt.Printf("ct ws: would remove %s\n", path)
			continue
		}
		if err := fsx.RemoveAll(path); err != nil {
			return fmt.Errorf("tidy-clean: %w", err)
		}
		fmt.Printf("ct ws: removed %s\n", path)
	}
	if kept > 0 {
		return fmt.Errorf("tidy-clean: kept %d clone(s)", kept)
	}
	return nil
}

// tidyUnmerged is why the scratch clone at path isn't safe to drop, "" when
// its bookmark is already on the ticket worktree's branch.
func tidyUnmerged(repos map[string]workspace.Repo, slug, path string) string {
	r, ok := repos[slug]
	if !ok {
		return "no repo " + slug + " in the workspace"
	}
	// without its own .git, git would read a parent repository's refs
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		return "no bookmark " + r.Branch
	}
	// read the ref in Go: the scratch clone's origin is the worktree's local
	// path, which ct's host git refuses to run in
	sha := readBranchRef(filepath.Join(path, ".git"), r.Branch)
	if sha == "" {
		return "no bookmark " + r.Branch
	}
	if !gitx.Ok(r.Path, "merge-base", "--is-ancestor", sha, "refs/heads/"+r.Branch) {
		short := sha
		if len(short) > 7 {
			short = short[:7]
		}
		return fmt.Sprintf("bookmark %s isn't on %s", short, r.Branch)
	}
	return ""
}

// readBranchRef is the commit refs/heads/<branch> names in the git dir, from
// the loose ref or packed-refs, "" when there is none (or the dir isn't one).
func readBranchRef(gitDir, branch string) string {
	if fi, err := os.Lstat(gitDir); err != nil || !fi.IsDir() {
		return ""
	}
	ref := "refs/heads/" + branch
	if b, ok := readRefFile(filepath.Join(gitDir, filepath.FromSlash(ref)), 1<<10); ok {
		if sha := strings.TrimSpace(string(b)); isHexSHA(sha) {
			return sha
		}
	}
	b, ok := readRefFile(filepath.Join(gitDir, "packed-refs"), 64<<20)
	if !ok {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if sha, name, ok := strings.Cut(strings.TrimSpace(line), " "); ok && name == ref && isHexSHA(sha) {
			return sha
		}
	}
	return ""
}

// readRefFile reads a file of the scratch clone (which a session can
// write) only when it is a regular file, not a link, FIFO or device, of at
// most max bytes; the file opened must be the one checked.
func readRefFile(path string, max int64) ([]byte, bool) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > max {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !os.SameFile(fi, st) {
		return nil, false
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(b)) > max {
		return nil, false
	}
	return b, true
}

func isHexSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

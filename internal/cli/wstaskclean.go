package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zero4573/tix/internal/fsx"
	"github.com/zero4573/tix/internal/gitx"
	"github.com/zero4573/tix/internal/vault"
	"github.com/zero4573/tix/internal/workspace"
)

func wsTaskCleanCmd() *cobra.Command {
	var force, dry bool
	cmd := &cobra.Command{
		Use:   "task-clean <ID> [--force] [--dry-run]",
		Short: "Remove a ticket's merged task worktrees and branches (on the host)",
		Long: `Removes the task worktrees under <workRoot>/<ID>/tasks/ that a session
couldn't (in the sandbox, the read-only mounts on a worktree that existed
when the session started keep it busy: "Device or resource busy"). Run it on
the host. For each repo of the workspace it goes through the main clone's
worktrees on a task/<ID>-* branch inside tasks/ (the tidy skill's tidy-*
clones are tix ws tidy-clean's):

  - merged (the branch tip is on the ticket worktree's branch) and clean:
    the worktree goes and the branch is deleted
  - unmerged or with uncommitted changes: kept with a warning; --force
    removes the worktree (uncommitted changes are lost) but keeps an
    unmerged branch

Then git worktree prune. Nothing else is touched.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeWorkspaceID,
		RunE: func(cmd *cobra.Command, args []string) error {
			if gitx.InContainer() {
				return errors.New("task-clean: run it on the host: in the sandbox its mounts keep the task worktrees busy")
			}
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			return wsTaskClean(ctx, args[0], force, dry)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "remove unmerged or dirty task worktrees too (unmerged branches are kept)")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "show what would be removed")
	return cmd
}

// taskWorktree is one entry of git worktree list --porcelain.
type taskWorktree struct{ path, branch string }

func listWorktrees(main string) ([]taskWorktree, error) {
	out, err := gitx.OutIn(main, nil, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var wts []taskWorktree
	var cur *taskWorktree
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "worktree "):
			wts = append(wts, taskWorktree{path: strings.TrimPrefix(l, "worktree ")})
			cur = &wts[len(wts)-1]
		case strings.HasPrefix(l, "branch ") && cur != nil:
			cur.branch = strings.TrimPrefix(strings.TrimPrefix(l, "branch "), "refs/heads/")
		}
	}
	return wts, nil
}

func wsTaskClean(ctx vault.Context, key string, force, dry bool) error {
	if err := requireKey("task-clean", key); err != nil {
		return err
	}
	dir := filepath.Join(ctx.WorkRoot, key)
	if !fsx.IsDir(dir) {
		return fmt.Errorf("task-clean: no workspace at %s", dir)
	}
	info, err := workspace.Read(dir)
	if err != nil {
		return fmt.Errorf("task-clean: %w", err)
	}
	tasks := filepath.Join(dir, "tasks")
	if real, err := filepath.EvalSymlinks(tasks); err == nil {
		tasks = real
	}
	act := ""
	if dry {
		act = "would "
	}
	kept, seen := 0, 0
	for _, r := range info.Repos {
		main := filepath.Join(ctx.ProjectsRoot, filepath.FromSlash(r.Clone()))
		wts, err := listWorktrees(main)
		if err != nil {
			warnf("task-clean: can't list the worktrees of %s: %v", main, err)
			kept++
			continue
		}
		sort.Slice(wts, func(i, j int) bool { return wts[i].path < wts[j].path })
		for _, wt := range wts {
			// only task worktrees of this ticket, inside its tasks/ (the
			// path comes from the clone's admin files, which a session can
			// write: never act outside tasks/)
			if !strings.HasPrefix(wt.branch, "task/"+key+"-") {
				continue
			}
			p := filepath.Clean(wt.path)
			if real, err := filepath.EvalSymlinks(p); err == nil {
				p = real
			} else if !os.IsNotExist(err) {
				continue
			}
			if !fsx.Inside(p, tasks) || strings.HasPrefix(filepath.Base(p), "tidy-") {
				continue
			}
			seen++
			tip, err := gitx.Out(main, "rev-parse", "-q", "--verify", "refs/heads/"+wt.branch)
			merged := err == nil && tip != "" && gitx.Ok(main, "merge-base", "--is-ancestor", tip, "refs/heads/"+r.Branch)
			var reasons []string
			if !merged {
				reasons = append(reasons, fmt.Sprintf("branch %s isn't on %s", wt.branch, r.Branch))
			}
			if fsx.IsDir(p) {
				modified, untracked, err := dirtyItems(p)
				switch {
				case err != nil:
					reasons = append(reasons, "uncommitted changes git can't list ("+err.Error()+")")
				case len(modified)+len(untracked) > 0:
					reasons = append(reasons, "uncommitted changes: "+listNames(append(modified, untracked...)))
				}
			}
			if len(reasons) > 0 && !force {
				warnf("%s kept: %s (use --force)", p, strings.Join(reasons, "; "))
				kept++
				continue
			}
			branch := "kept"
			if merged {
				branch = "deleted (merged)"
			}
			if dry {
				fmt.Printf("tix ws: %sremove %s (branch %s %s)\n", act, p, wt.branch, branch)
				continue
			}
			if fsx.IsDir(p) {
				// checked above, so always --force (gitx refuses it without)
				if err := gitx.Run(main, "worktree", "remove", "--force", p); err != nil {
					warnf("task-clean: couldn't remove the worktree %s", p)
					kept++
					continue
				}
			}
			if merged {
				// only the tip that was checked: a branch moved since stays
				if err := gitx.Run(main, "update-ref", "-d", "refs/heads/"+wt.branch, tip); err != nil {
					warnf("task-clean: removed %s but couldn't delete the branch %s", p, wt.branch)
					kept++
					continue
				}
			}
			fmt.Printf("tix ws: removed %s (branch %s %s)\n", p, wt.branch, branch)
		}
		if !dry {
			_ = gitx.Timeout(time.Minute, main, "worktree", "prune")
		}
	}
	if seen == 0 {
		fmt.Printf("tix ws: no task worktrees of %s in %s\n", key, tasks)
	}
	if kept > 0 {
		return fmt.Errorf("task-clean: kept %d task worktree(s)", kept)
	}
	return nil
}

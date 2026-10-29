package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

func wsMarkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mark <ID> <tidy> [<slug>...]",
		Short: "Record that tidy was done at the repos' current commit",
		Long: `Records HEAD, its tree and the time as the ticket's tidy mark of
the named repos (default: all of the workspace). A repo with uncommitted
changes is skipped with a warning, and the command exits 1. ct ws marks
shows where the marks stand; /tickets:save checks them before it pushes.`,
		Args: cobra.MinimumNArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, s string) ([]string, cobra.ShellCompDirective) {
			switch len(args) {
			case 0:
				return completeWorkspaceID(cmd, args, s)
			case 1:
				return workspace.MarkKinds, cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			return wsMark(ctx, args[0], args[1], args[2:])
		},
	}
}

func wsMark(ctx vault.Context, key, kind string, slugs []string) error {
	if err := requireKey("mark", key); err != nil {
		return err
	}
	if !workspace.ValidMark(kind) {
		return fmt.Errorf("mark: unknown kind '%s' (tidy)", kind)
	}
	dir := filepath.Join(ctx.WorkRoot, key)
	if _, err := os.Stat(workspace.File(dir)); err != nil {
		return fmt.Errorf("mark: %s has no workspace (%s)", key, dir)
	}
	repos := wsRepos(ctx, key)
	if len(slugs) > 0 {
		var pick []workspace.Repo
		for _, s := range slugs {
			found := false
			for _, r := range repos {
				if r.Slug == s {
					pick, found = append(pick, r), true
				}
			}
			if !found {
				return fmt.Errorf("mark: %s has no repo '%s' in its workspace", key, s)
			}
		}
		repos = pick
	}
	failed := false
	for _, r := range repos {
		if _, err := os.Stat(filepath.Join(r.Path, ".git")); err != nil {
			warnf("mark: %s: worktree missing (%s)", r.Slug, r.Path)
			failed = true
			continue
		}
		if gitx.Dirty(r.Path) {
			wsWarn("%s: uncommitted changes, not marked", r.Slug)
			failed = true
			continue
		}
		commit, err1 := gitx.Out(r.Path, "rev-parse", "HEAD")
		tree, err2 := gitx.Out(r.Path, "rev-parse", "HEAD^{tree}")
		if err1 != nil || err2 != nil {
			wsWarn("%s: no commit to mark", r.Slug)
			failed = true
			continue
		}
		m := workspace.Mark{Commit: commit, Tree: tree, At: time.Now().UTC().Format(time.RFC3339)}
		if err := workspace.SetMark(workspace.File(dir), r.Slug, kind, m); err != nil {
			return err
		}
		fmt.Printf("ct ws: %s: %s marked at %s\n", r.Slug, kind, shortSHA(commit))
	}
	if failed {
		return exitError(1)
	}
	return nil
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// markState is a repo's status per mark kind ("missing" for both when the
// worktree is gone), with the current HEAD for the warnings.
type markState struct {
	Repo    workspace.Repo
	Missing bool
	Status  map[string]string
}

func (m markState) ok() bool {
	if m.Missing {
		return false
	}
	for _, k := range workspace.MarkKinds {
		if m.Status[k] != workspace.MarkOK {
			return false
		}
	}
	return true
}

func wsMarkStates(repos []workspace.Repo) []markState {
	var out []markState
	for _, r := range repos {
		st := markState{Repo: r, Status: map[string]string{}}
		tree, err := gitx.Out(r.Path, "rev-parse", "HEAD^{tree}")
		if err != nil {
			st.Missing = true
		} else {
			for _, k := range workspace.MarkKinds {
				st.Status[k] = workspace.MarkStatus(r, k, tree)
			}
		}
		out = append(out, st)
	}
	return out
}

func wsMarksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "marks <ID>",
		Short: "Show whether tidy was done after each repo's last commit",
		Long: `One line per repo: <slug> tidy <ok|stale|none>. ok:
the marked tree is the repo's current one; stale: commits came after the
mark; none: not marked (ct ws mark). Exits 0 when every repo is ok,
else 3.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeWorkspaceID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			key := args[0]
			if err := requireKey("marks", key); err != nil {
				return err
			}
			if _, err := os.Stat(workspace.File(filepath.Join(ctx.WorkRoot, key))); err != nil {
				return fmt.Errorf("marks: %s has no workspace (%s)", key, filepath.Join(ctx.WorkRoot, key))
			}
			all := true
			for _, st := range wsMarkStates(wsRepos(ctx, key)) {
				if st.Missing {
					fmt.Printf("%s tidy missing\n", st.Repo.Slug)
				} else {
					fmt.Printf("%s tidy %s\n", st.Repo.Slug, st.Status[workspace.MarkTidy])
				}
				all = all && st.ok()
			}
			if !all {
				return exitError(3)
			}
			return nil
		},
	}
}

func wsWarn(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ct ws: warning: "+format+"\n", a...)
}

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zero4573/tix/internal/fsx"
	"github.com/zero4573/tix/internal/gitx"
	"github.com/zero4573/tix/internal/note"
	"github.com/zero4573/tix/internal/repo"
	"github.com/zero4573/tix/internal/spec"
	"github.com/zero4573/tix/internal/vault"
	"github.com/zero4573/tix/internal/vaultlock"
	"github.com/zero4573/tix/internal/workspace"
)

// pinNow is the clock of tix spec pin (a seam for tests).
var pinNow = time.Now

func specPinCmd() *cobra.Command {
	var refresh string
	cmd := &cobra.Command{
		Use:   "pin <ID> | pin --refresh <slug>",
		Short: "Pin a ticket's vault spec versions to the pushed commit",
		Long: `tix spec pin <ID>: for every repo of the ticket's workspace whose specs live in
the vault and that has a version note, pins the note to the ticket branch's
head when the push landed (the worktree's origin/<branch> is its <branch>),
then resolves what it can (merged, tags). Prints per repo "<slug>: pinned
<sha>", "<slug>: already pinned" or "<slug>: not pushed, left unpinned".
Pushing a rewritten branch again re-pins it.

tix spec pin --refresh <slug>: for every pinned version of the slug, in its
main clone (fetch first, tix ws fetch): merged is the pinned commit when the
base holds it (a merge), else the first commit on the base since the pin
with the branch's patch-id (a squash; one with conflict fixes isn't found),
and tags are the tags that hold it, oldest version first.

Both write the version notes under the vault lock and fail, naming the
holder, when another session holds it.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if refresh != "" {
				return cobra.NoArgs(cmd, args)
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			if refresh != "" {
				return pinRefresh(cmd, ctx, refresh)
			}
			return pinTicket(cmd, ctx, args[0])
		},
	}
	cmd.Flags().StringVar(&refresh, "refresh", "", "resolve merged and tags of every pinned version of `slug` in its main clone")
	return cmd
}

// pinLock takes the vault lock for owner, failing at once (no wait) when
// another owner holds it, with then saying what that leaves (default: the
// next save pins).
type pinLock struct {
	vault, owner, then string
	held               bool
}

func (l *pinLock) take() error {
	if l.held {
		return nil
	}
	if err := vaultlock.TryAcquire(l.vault, l.owner); err != nil {
		then := l.then
		if then == "" {
			then = "versions left as they are (the next save pins them)"
		}
		return fmt.Errorf("%w; %s", err, then)
	}
	l.held = true
	return nil
}

func (l *pinLock) release() {
	if l.held {
		_ = vaultlock.Release(l.vault, l.owner)
	}
}

// writeVersionNote rewrites a version note whole from v and its ticket's
// snapshots.
func writeVersionNote(ctx vault.Context, path string, v spec.Version) error {
	before, after, err := spec.VersionSides(ctx.Vault, v.Slug, v)
	if err != nil {
		return err
	}
	body, err := spec.RenderVersion(v, note.RelDir(ctx.Vault, v.ID()), before, spec.DiffTrees(v.Slug, before, after))
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(path, body, 0o644)
}

func pinTicket(cmd *cobra.Command, ctx vault.Context, id string) error {
	if !note.ValidKey(id) {
		return fmt.Errorf("invalid ticket ID %q", id)
	}
	info, err := workspace.Read(filepath.Join(ctx.WorkRoot, id))
	if err != nil {
		return fmt.Errorf("no workspace for %s: %w", id, err)
	}
	lk := &pinLock{vault: ctx.Vault, owner: "tix spec pin " + id}
	defer lk.release()
	out := cmd.OutOrStdout()
	for _, r := range info.Repos {
		loc, err := spec.Resolve(ctx.Vault, r.Slug, r.Path)
		if err != nil {
			return fmt.Errorf("%s: %w", r.Slug, err)
		}
		if loc.Kind != spec.InVault {
			continue
		}
		path := spec.VersionPath(ctx.Vault, r.Slug, id)
		v, err := spec.ReadVersion(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("%s: %w", r.Slug, err)
		}
		if err := spec.Pending(ctx.Vault, id, r.Slug); spec.PendingNote(err, id, r.Slug) != "" {
			fmt.Fprintf(out, "%s: %s, left as it is\n", r.Slug, spec.PendingNote(err, id, r.Slug))
			continue
		} else if err != nil {
			return fmt.Errorf("%s: %w", r.Slug, err)
		}
		sha, ok, err := spec.PushedHead(r.Path, r.Branch)
		if err != nil {
			return fmt.Errorf("%s: %w", r.Slug, err)
		}
		if !ok {
			fmt.Fprintf(out, "%s: not pushed, left unpinned\n", r.Slug)
			continue
		}
		base := gitx.BaseRef(r.Path, r.Base)
		pinned := spec.Pin(&v, sha, pinNow())
		resolved, err := spec.Refresh(r.Path, base, &v)
		if err != nil {
			return fmt.Errorf("%s: %w", r.Slug, err)
		}
		if pinned || resolved {
			if err := lk.take(); err != nil {
				return err
			}
			// Another save may have rewritten the note since it was read:
			// apply the change again to what is there under the lock.
			var changed bool
			v, pinned, changed, err = spec.Reapply(path, r.Path, base, sha, pinNow())
			if err != nil {
				return fmt.Errorf("%s: %w", r.Slug, err)
			}
			if changed {
				if err := writeVersionNote(ctx, path, v); err != nil {
					return fmt.Errorf("%s: %w", r.Slug, err)
				}
			}
		}
		if pinned {
			fmt.Fprintf(out, "%s: pinned %s\n", r.Slug, shortSHA(sha))
		} else {
			fmt.Fprintf(out, "%s: already pinned\n", r.Slug)
		}
	}
	return nil
}

// mainClone is the main clone of slug (a slug or <provider>/<owner>/<repo>):
// the slug, and the clone's path ("" when there is none).
func mainClone(ctx vault.Context, arg string) (slug, dir string) {
	if c, ok := repo.Resolve(ctx.ProjectsRoot, arg); ok {
		return repo.Slug(c), filepath.Join(ctx.ProjectsRoot, filepath.FromSlash(c))
	}
	return arg, ""
}

// cloneBase is the base a version of slug is resolved against in the main
// clone dir: its ticket workspace's base for the repo when the workspace is
// still there, else the clone's default branch (origin/HEAD, else its
// checked-out branch). The name, and the ref to use (gitx.BaseRef).
func cloneBase(ctx vault.Context, dir, id, slug string) (name, ref string) {
	if id != "" {
		if info, err := workspace.Read(filepath.Join(ctx.WorkRoot, id)); err == nil {
			for _, r := range info.Repos {
				if r.Slug == slug && r.Base != "" {
					return r.Base, gitx.BaseRef(dir, r.Base)
				}
			}
		}
	}
	if s, err := gitx.Out(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && s != "" {
		name = strings.TrimPrefix(s, "origin/")
	} else if s, err := gitx.Out(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && s != "" && s != "HEAD" {
		name = s
	} else {
		name = "main"
	}
	return name, gitx.BaseRef(dir, name)
}

func pinRefresh(cmd *cobra.Command, ctx vault.Context, arg string) error {
	slug, dir := mainClone(ctx, arg)
	if dir == "" {
		return fmt.Errorf("no main clone '%s' (see tix ws repos)", arg)
	}
	loc, err := spec.Resolve(ctx.Vault, slug, dir)
	if err != nil {
		return err
	}
	if loc.Kind != spec.InVault {
		return fmt.Errorf("%s: its specs are not in the vault (tix spec location %s)", slug, slug)
	}
	vs, err := spec.ListVersions(ctx.Vault, slug)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(vs) == 0 {
		fmt.Fprintf(out, "%s: no spec versions\n", slug)
		return nil
	}
	lk := &pinLock{vault: ctx.Vault, owner: "tix spec pin --refresh " + slug}
	defer lk.release()
	for _, vf := range vs {
		v := vf.Version
		if v.Commit == "" {
			fmt.Fprintf(out, "%s %s: unpinned\n", slug, v.ID())
			continue
		}
		if err := spec.Pending(ctx.Vault, v.ID(), slug); spec.PendingNote(err, v.ID(), slug) != "" {
			fmt.Fprintf(out, "%s %s: %s, skipped\n", slug, v.ID(), spec.PendingNote(err, v.ID(), slug))
			continue
		} else if err != nil {
			return fmt.Errorf("%s %s: %w", slug, v.ID(), err)
		}
		base, ref := cloneBase(ctx, dir, v.ID(), slug)
		changed, err := spec.Refresh(dir, ref, &v)
		if err != nil {
			return fmt.Errorf("%s %s: %w", slug, v.ID(), err)
		}
		if changed {
			if err := lk.take(); err != nil {
				return err
			}
			// Re-read under the lock: another save may have rewritten the
			// note (new files, pin cleared) since it was listed.
			v, _, changed, err = spec.Reapply(vf.Path, dir, ref, "", pinNow())
			if err != nil {
				return fmt.Errorf("%s %s: %w", slug, v.ID(), err)
			}
			if changed {
				if err := writeVersionNote(ctx, vf.Path, v); err != nil {
					return fmt.Errorf("%s %s: %w", slug, v.ID(), err)
				}
			}
		}
		fmt.Fprintf(out, "%s %s: %s\n", slug, v.ID(), mergeState(v, base))
	}
	return nil
}

// mergeState says where a pinned version is: "merged <sha>[, in <tags>]"
// or "not found on <base>[, in <tags>]".
func mergeState(v spec.Version, base string) string {
	s := "not found on " + base
	if v.Merged != "" {
		s = "merged " + shortSHA(v.Merged)
	}
	if len(v.Tags) > 0 {
		s += ", in " + strings.Join(v.Tags, " ")
	}
	return s
}

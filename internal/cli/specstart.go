package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zero4573/tix/internal/config"
	"github.com/zero4573/tix/internal/fsx"
	"github.com/zero4573/tix/internal/note"
	"github.com/zero4573/tix/internal/spec"
	"github.com/zero4573/tix/internal/vault"
)

// startResult is what tix spec start did for one ticket and repo.
type startResult struct {
	Copy     string         // the copy folder, vault-relative with a trailing /
	Snapshot string         // the snapshot folder, vault-relative with a trailing /
	Taken    bool           // the snapshot was taken by this run (else kept)
	Copied   []string       // the files copied, sorted
	Kept     []string       // the files already in the copy, left as they were
	Rebased  []string       // the files whose snapshot entries moved onto projects/, sorted
	Moved    []string       // of Rebased, the promoted files projects/ changed since
	Orphan   bool           // a rebase marker without a base snapshot was cleared
	Reset    bool           // an after snapshot left without its base was reset
	Replay   *promoteResult // a pending promote this run finished
}

// printStartReplay prints the pending promote start finished (rp, nil:
// none) and, when it held files back, what resolves them.
func printStartReplay(out io.Writer, id string, rp *promoteResult) {
	if rp == nil {
		return
	}
	printPromote(out, id, *rp)
	if len(rp.Held) > 0 {
		fmt.Fprintf(out, "run tix spec promote %s to resolve\n", id)
	}
}

// startChecked runs between tix spec start's first check for a pending
// promote and what follows it (a seam for tests).
var startChecked = func() {}

// startReplay finishes ticket id's pending promote of slug, if any, under
// the vault lock: it fails at once when another session holds the lock,
// checks again under it, and releases it before start's own work. Without
// a journal it leaves any stage as it is (only promote removes one); a
// journal that appears after that check is left for promote.
func startReplay(ctx vault.Context, id, slug string) (*promoteResult, error) {
	pending, err := exists(spec.JournalPath(ctx.Vault, id, slug))
	if err != nil {
		return nil, err
	}
	startChecked()
	if !pending {
		return nil, spec.PendingPromote(ctx.Vault, id, slug)
	}
	lk := &pinLock{vault: ctx.Vault, owner: "tix spec start " + id,
		then: fmt.Sprintf("nothing written (a promote of %s is pending and needs it: try again, or run tix spec promote %s)", id, id)}
	if err := lk.take(); err != nil {
		return nil, err
	}
	defer lk.release()
	if pending, err = exists(spec.JournalPath(ctx.Vault, id, slug)); err != nil || !pending {
		return nil, err
	}
	return finishPromote(ctx.Vault, id, slug)
}

func specStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <ID> <slug> [<capability>...]",
		Short: "Make a ticket's copy of a vault spec and snapshot projects/ for promote",
		Long: `Makes the ticket's copy folder tickets/<source>/<ID>/specs/<slug>/ of a
vault-located spec (the project note says specs: vault) and copies the named
capabilities into it from projects/<slug>/specs/ (none: an empty copy, for
new capabilities). A file already in the copy is kept as it is.
The first run also writes tickets/<source>/<ID>/specs-base/<slug>/, an
atomic snapshot of the whole projects/<slug>/specs/ as it is then. A
repeat run adds capabilities; for each file it copies (one the copy
doesn't have) it first rebases the snapshot onto projects/'s current file,
so a change another ticket promoted before that start is not this
ticket's change, and says so ("rebased <file>"). A file the ticket already
promoted keeps its snapshot, and so its promoted change in tix spec diff,
unless another ticket changed it in projects/ since: then it is rebased
too and that earlier change leaves the diff, the version history (tix spec
log) and the version's view (tix spec show). A run stopped between its two
snapshot writes is finished by the next tix spec start. A promote of the
ticket that stopped after its commit point is finished first, under the
vault lock (failing at once when another session holds it); the files it
held back are listed, for tix spec promote <ID> to resolve. tix spec
promote compares projects/ against it, so a change another ticket
promotes after this start is refused instead of overwritten; it refuses
a copy that has no snapshot.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			res, err := specStart(ctx, args[0], args[1], args[2:])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			printStartReplay(out, args[0], res.Replay)
			printStart(out, args[1], res)
			return nil
		},
	}
}

// oneName reports whether s is a single, plain path element.
func oneName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`) && filepath.Base(s) == s
}

// specStart makes the copy of slug's vault spec for ticket id with caps
// copied in, and the snapshot when there is none (see specStartCmd).
// Everything is checked before anything is written.
func specStart(ctx vault.Context, id, slug string, caps []string) (startResult, error) {
	var res startResult
	if !note.ValidKey(id) {
		return res, fmt.Errorf("invalid ticket ID %q", id)
	}
	if note.Find(ctx.Vault, id) == "" {
		return res, fmt.Errorf("no ticket %s in the vault", id)
	}
	if !oneName(slug) {
		return res, fmt.Errorf("invalid repo slug %q", slug)
	}
	if f := note.Get(spec.ProjectNote(ctx.Vault, slug), "specs"); f != spec.InVault {
		return res, fmt.Errorf("%s: its specs are not in the vault (%s needs specs: vault; tix spec location %s)",
			slug, config.TildePath(spec.ProjectNote(ctx.Vault, slug)), slug)
	}
	// a promote of this ticket that passed its commit point and stopped
	// is finished first, under the vault lock, so the snapshots read below
	// are whole
	rp, err := startReplay(ctx, id, slug)
	if err != nil {
		return res, err
	}
	res.Replay = rp
	projects, err := spec.ReadTree(spec.VaultDir(ctx.Vault, slug))
	if err != nil {
		return res, err
	}
	var files []string
	for _, c := range caps {
		if !oneName(c) {
			return res, fmt.Errorf("invalid capability %q", c)
		}
		n := 0
		for _, f := range projects.Keys() {
			if strings.HasPrefix(f, c+"/") {
				files = append(files, f)
				n++
			}
		}
		if n == 0 {
			return res, fmt.Errorf("%s: no capability %s in projects/%s/specs/ (a new one: run tix spec start %s %s without it first, then write it in the copy folder)", slug, c, slug, id, slug)
		}
	}
	sort.Strings(files)

	copyDir, baseDir := spec.CopyDir(ctx.Vault, id, slug), spec.BaseDir(ctx.Vault, id, slug)
	res.Copy = vaultRel(ctx.Vault, copyDir) + "/"
	res.Snapshot = vaultRel(ctx.Vault, baseDir) + "/"
	res.Copied, res.Kept = []string{}, []string{}
	// the snapshot first: a copy never exists without one
	base, err := spec.ReadSnapshot(baseDir)
	if err != nil {
		return res, err
	}
	if base == nil {
		// a base removed by hand (with or without a rebase marker beside
		// it) rebases nothing: the fresh base is projects/ as it is now,
		// and an after snapshot left without its base has its entries (the
		// ticket's files only) reset to it first, so a file another ticket
		// changed since this ticket promoted it doesn't read as this ticket
		// reverting that change. The marker goes last: a run that stops
		// before that does the same again.
		marker := spec.RebaseMarker(ctx.Vault, id, slug)
		_, err := os.Lstat(marker)
		orphan := err == nil
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return res, err
		}
		afterDir := spec.AfterDir(ctx.Vault, id, slug)
		after, err := spec.ReadSnapshot(afterDir)
		if err != nil {
			return res, err
		}
		if after != nil {
			if err := writeSnapshot(afterDir, spec.Pick(projects, after.Keys())); err != nil {
				return res, err
			}
			res.Reset = true
		}
		if err := spec.WriteTree(baseDir, projects); err != nil {
			return res, err
		}
		res.Taken = true
		if orphan {
			if err := os.Remove(marker); err != nil {
				return res, err
			}
			res.Orphan = true
		}
	}
	// per file: one the copy holds is kept, one it lacks is copied
	var toCopy []string
	seen := map[string]bool{}
	for _, f := range files {
		if seen[f] {
			continue
		}
		seen[f] = true
		if _, err := os.Lstat(filepath.Join(copyDir, filepath.FromSlash(f))); err == nil {
			res.Kept = append(res.Kept, f)
		} else if errors.Is(err, os.ErrNotExist) {
			toCopy = append(toCopy, f)
		} else {
			return res, err
		}
	}
	if base != nil {
		if err := rebaseStart(ctx, id, slug, projects, base, toCopy, &res); err != nil {
			return res, err
		}
	}
	if err := os.MkdirAll(copyDir, 0o755); err != nil {
		return res, err
	}
	for _, f := range toCopy {
		dst := filepath.Join(copyDir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return res, err
		}
		if err := fsx.WriteFileAtomic(dst, projects[f], 0o644); err != nil {
			return res, err
		}
		res.Copied = append(res.Copied, f)
	}
	return res, nil
}

// writeSnapshot writes a snapshot folder; a seam for the failure tests.
var writeSnapshot = spec.WriteTree

// rebaseStart rebases the snapshot entries of the files a repeat start is
// about to copy (toCopy) onto projects/, so neither tix spec diff nor tix spec
// promote takes another ticket's change made before this start for this
// ticket's. A file the ticket's version note lists as promoted keeps its
// entries (they hold the ticket's promoted change) unless projects/ changed
// it since that promotion; then it is rebased too, and its earlier change
// leaves the diff, the version history (tix spec log) and the version's view
// (tix spec show). The snapshots are written before any copy file: first the
// rebase marker naming the files, then base, then after, then the marker is
// removed. A run that stops between the two snapshots leaves the marker;
// tix spec promote refuses while it is there, and the next tix spec start,
// whatever capabilities it names, rebases the marker's files again.
func rebaseStart(ctx vault.Context, id, slug string, projects, base spec.Tree, toCopy []string, res *startResult) error {
	marker := spec.RebaseMarker(ctx.Vault, id, slug)
	pending, err := readRebaseMarker(marker)
	if err != nil {
		return err
	}
	if len(toCopy) == 0 && len(pending) == 0 {
		return nil
	}
	afterDir := spec.AfterDir(ctx.Vault, id, slug)
	after, err := spec.ReadSnapshot(afterDir)
	if err != nil {
		return err
	}
	v, err := spec.ReadVersion(spec.VersionPath(ctx.Vault, slug, id))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var files []string
	since := map[string]bool{}
	for _, f := range pending {
		files = append(files, f)
		since[f] = v.HasFile(f) && after != nil && !spec.SameFile(after, projects, f)
	}
	for _, f := range toCopy {
		switch {
		case !v.HasFile(f):
			files = append(files, f)
		case after != nil && !spec.SameFile(after, projects, f):
			files = append(files, f)
			since[f] = true
		}
	}
	nb, na, changed := spec.Rebase(projects, base, after, files)
	if len(changed) == 0 {
		return removeRebaseMarker(marker, pending)
	}
	if err := fsx.WriteFileAtomic(marker, []byte(strings.Join(changed, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	interrupted := func(err error) error {
		return fmt.Errorf("%w (the snapshots are part way rebased: run tix spec start %s %s again to finish them)", err, id, slug)
	}
	if err := writeSnapshot(spec.BaseDir(ctx.Vault, id, slug), nb); err != nil {
		return interrupted(err)
	}
	if after != nil {
		if err := writeSnapshot(afterDir, na); err != nil {
			return interrupted(err)
		}
	}
	if err := os.Remove(marker); err != nil {
		return err
	}
	res.Rebased = changed
	for _, f := range changed {
		if since[f] {
			res.Moved = append(res.Moved, f)
		}
	}
	return nil
}

// readRebaseMarker lists the files of a rebase a tix spec start left part
// way (see rebaseStart); none without a marker.
func readRebaseMarker(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

// removeRebaseMarker removes the marker when one was read (pending).
func removeRebaseMarker(path string, pending []string) error {
	if len(pending) == 0 {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func printStart(out io.Writer, slug string, res startResult) {
	if res.Orphan {
		fmt.Fprintf(out, "%s: warning: cleared a rebase marker left without its snapshot (the snapshot was taken again from projects/, and the after snapshot reset to it)\n", slug)
	}
	if res.Reset && !res.Orphan {
		fmt.Fprintf(out, "%s: warning: the snapshot was missing and taken again from projects/, and the after snapshot reset to it\n", slug)
	}
	switch {
	case res.Taken:
		fmt.Fprintf(out, "%s: snapshot %s taken\n", slug, res.Snapshot)
	case len(res.Rebased) > 0:
		fmt.Fprintf(out, "%s: snapshot %s kept (taken by the first start) but for the files rebased below\n", slug, res.Snapshot)
	default:
		fmt.Fprintf(out, "%s: snapshot %s kept (taken by the first start)\n", slug, res.Snapshot)
	}
	moved := map[string]bool{}
	for _, f := range res.Moved {
		moved[f] = true
	}
	for _, f := range res.Rebased {
		if moved[f] {
			fmt.Fprintf(out, "  rebased %s in the snapshot (projects/ changed it since this ticket promoted it: its earlier change leaves tix spec diff, tix spec log and tix spec show)\n", f)
		} else {
			fmt.Fprintf(out, "  rebased %s in the snapshot onto projects/\n", f)
		}
	}
	fmt.Fprintf(out, "%s: copy %s\n", slug, res.Copy)
	for _, f := range res.Copied {
		fmt.Fprintf(out, "  copied %s\n", f)
	}
	for _, f := range res.Kept {
		fmt.Fprintf(out, "  kept %s (already in the copy)\n", f)
	}
}

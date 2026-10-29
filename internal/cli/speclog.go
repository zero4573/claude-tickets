package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/spec"
	"github.com/zero4573/claude-tickets/internal/vault"
)

// Where a version stands, as ct spec log reads it.
const (
	stateUnpinned = "unpinned"
	statePinned   = "pinned"    // pinned, no main clone to resolve it in
	stateMerged   = "merged"    // the base holds it (merged is set)
	stateNotFound = "not-found" // pinned, not found on the base
)

// logVersion is one version as ct spec log prints it: the note's fields,
// resolved at read time (nothing is written), and its change.
type logVersion struct {
	ID   string `json:"id"`
	Note string `json:"note"`
	spec.Version
	Base  string `json:"base,omitempty"`
	State string `json:"state"`
	// Refresh is set while this ticket's rebase marker or promote journal
	// is there: its change isn't read, and the note says how to finish it.
	Refresh string    `json:"refresh,omitempty"`
	Change  spec.Diff `json:"change"`
}

type logCommit struct {
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

// specSlug finds where slug's specs live; dir is its main clone ("" for
// none, when only the project note's specs: vault can say).
func specSlug(ctx vault.Context, arg string) (slug, dir string, loc spec.Location, err error) {
	slug, dir = mainClone(ctx, arg)
	if dir != "" {
		loc, err = spec.Resolve(ctx.Vault, slug, dir)
		if err != nil {
			return slug, dir, loc, err
		}
	} else if note.Get(spec.ProjectNote(ctx.Vault, slug), "specs") == spec.InVault {
		loc = spec.Location{Kind: spec.InVault, Dir: spec.VaultDir(ctx.Vault, slug), Field: spec.InVault}
	}
	if loc.Kind == spec.Unset {
		return slug, dir, loc, fmt.Errorf("%s: no specs (no specs: field in %s and no specs/ in a main clone; see ct spec location)", slug, vaultRel(ctx.Vault, spec.ProjectNote(ctx.Vault, slug)))
	}
	return slug, dir, loc, nil
}

// readVersions reads slug's versions, newest promoted first, resolved in
// the main clone dir (when there is one) without writing the notes.
func readVersions(ctx vault.Context, slug, dir string) ([]logVersion, error) {
	vs, err := spec.ListVersions(ctx.Vault, slug)
	if err != nil {
		return nil, err
	}
	out := make([]logVersion, 0, len(vs))
	for _, vf := range vs {
		lv := logVersion{ID: vf.Version.ID(), Note: vaultRel(ctx.Vault, vf.Path), Version: vf.Version, State: stateUnpinned}
		if lv.Commit != "" {
			lv.State = statePinned
			if dir != "" {
				var ref string
				lv.Base, ref = cloneBase(ctx, dir, lv.ID, slug)
				if _, err := spec.Refresh(dir, ref, &lv.Version); err != nil {
					return nil, fmt.Errorf("%s: %w", lv.ID, err)
				}
				lv.State = stateNotFound
			}
			if lv.Merged != "" {
				lv.State = stateMerged
			}
		}
		before, after, err := spec.VersionSides(ctx.Vault, slug, lv.Version)
		if n := spec.PendingNote(err, lv.ID, slug); n != "" {
			lv.Refresh = n
			lv.Change = spec.Diff{Slug: slug}
			out = append(out, lv)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", lv.ID, err)
		}
		lv.Change = spec.DiffTrees(slug, before, after)
		out = append(out, lv)
	}
	return out, nil
}

// pickCap narrows a version to one capability: false when it promoted no
// file of it.
func (lv *logVersion) pickCap(slug, capability string) bool {
	if capability == "" {
		return true
	}
	hit := false
	for _, f := range lv.Files {
		if strings.HasPrefix(f, capability+"/") {
			hit = true
		}
	}
	if !hit {
		return false
	}
	var caps []spec.CapChange
	for _, c := range lv.Change.Caps {
		if c.Capability == capability || strings.HasPrefix(c.File, capability+"/") {
			caps = append(caps, c)
		}
	}
	var files []spec.FileDiff
	for _, f := range lv.Change.Files {
		if strings.HasPrefix(f.Path, capability+"/") {
			files = append(files, f)
		}
	}
	lv.Change = spec.Diff{Slug: slug, Caps: caps, Files: files}
	return true
}

// compactChange is one line per changed capability: +ID added, -ID
// removed, ~ID changed, "ID from→to" a status flip, ~interfaces.
func compactChange(d spec.Diff) []string {
	var out []string
	for _, c := range d.Caps {
		var parts []string
		for _, ch := range c.Changes {
			switch ch.Kind {
			case spec.Added:
				parts = append(parts, "+"+ch.Req.ID)
			case spec.Removed:
				parts = append(parts, "-"+ch.Req.ID)
			case spec.Flipped:
				parts = append(parts, ch.Req.ID+" "+ch.From+"→"+ch.To)
			default:
				parts = append(parts, "~"+ch.Req.ID)
			}
		}
		if c.Interfaces != "" {
			parts = append(parts, "~interfaces")
		}
		if len(parts) > 0 {
			out = append(out, c.Capability+": "+strings.Join(parts, " "))
		}
	}
	return out
}

func plural(n int, one string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %ss", n, one)
}

func specLogCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "log <slug> [<capability>]",
		Short: "List a spec's versions",
		Long: `Lists a vault spec's versions, newest promotion first: per version the
ticket, the promotion date, the pinned commit and branch (or unpinned), where
the base holds it (merged <sha>, or not found on <base>) and its first tag,
then its requirement changes per capability (+ added, - removed, ~ changed,
status flips as from→to). Merged and tags are resolved in the main clone at
read time; the notes aren't written (ct spec pin --refresh does that).
With a capability, only the versions that promoted one of its files, and
that capability's change. --json prints the versions as data.
For a slug whose specs live in the repo, prints the base's git log of
specs/ (or specs/<capability>/) from the main clone instead.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			slug, dir, loc, err := specSlug(ctx, args[0])
			if err != nil {
				return err
			}
			capability := ""
			if len(args) == 2 {
				capability = strings.Trim(args[1], "/")
			}
			out := cmd.OutOrStdout()
			if loc.Kind == spec.InRepo {
				return repoSpecLog(out, ctx, slug, dir, capability, asJSON)
			}
			all, err := readVersions(ctx, slug, dir)
			if err != nil {
				return fmt.Errorf("%s: %w", slug, err)
			}
			vs := []logVersion{}
			for _, lv := range all {
				if lv.pickCap(slug, capability) {
					vs = append(vs, lv)
				}
			}
			if asJSON {
				b, err := json.MarshalIndent(map[string]any{"slug": slug, "location": spec.InVault, "versions": vs}, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(out, string(b))
				return nil
			}
			printVersionLog(out, slug, vs)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the versions as JSON")
	return cmd
}

func printVersionLog(out io.Writer, slug string, vs []logVersion) {
	if len(vs) == 0 {
		fmt.Fprintf(out, "%s: no spec versions\n", slug)
		return
	}
	fmt.Fprintf(out, "%s: vault specs, %s\n", slug, plural(len(vs), "version"))
	idw, pinw := 0, 0
	pin := func(lv logVersion) string { return shortSHA(lv.Commit) + " " + lv.Branch }
	for _, lv := range vs {
		idw = max(idw, len(lv.ID))
		if lv.Commit != "" {
			pinw = max(pinw, len(pin(lv)))
		}
	}
	for _, lv := range vs {
		date := lv.Promoted
		if len(date) > 10 {
			date = date[:10]
		}
		parts := []string{fmt.Sprintf("%-*s", idw, lv.ID), date}
		switch lv.State {
		case stateUnpinned:
			parts = append(parts, "unpinned")
		case statePinned:
			parts = append(parts, pin(lv))
		case stateMerged:
			parts = append(parts, fmt.Sprintf("%-*s", pinw, pin(lv)), "merged "+shortSHA(lv.Merged))
		case stateNotFound:
			parts = append(parts, fmt.Sprintf("%-*s", pinw, pin(lv)), "not found on "+lv.Base)
		}
		if len(lv.Tags) > 0 {
			parts = append(parts, lv.Tags[0])
		}
		fmt.Fprintln(out, strings.TrimRight(strings.Join(parts, "  "), " "))
		if lv.Refresh != "" {
			fmt.Fprintf(out, "%s%s\n", strings.Repeat(" ", idw+2), lv.Refresh)
		}
		for _, l := range compactChange(lv.Change) {
			fmt.Fprintf(out, "%s%s\n", strings.Repeat(" ", idw+2), l)
		}
	}
}

func repoSpecLog(out io.Writer, ctx vault.Context, slug, dir, capability string, asJSON bool) error {
	base, ref := cloneBase(ctx, dir, "", slug)
	path := "specs/"
	if capability != "" {
		path += capability + "/"
	}
	commits := []logCommit{}
	err := gitx.Lines(dir, func(l string) {
		f := strings.SplitN(l, " ", 3)
		if len(f) == 3 {
			commits = append(commits, logCommit{Commit: f[0], Date: f[1], Subject: f[2]})
		}
	}, "log", "--format=%h %cs %s", ref, "--", path)
	if err != nil {
		return fmt.Errorf("%s: %w", slug, err)
	}
	if asJSON {
		b, err := json.MarshalIndent(map[string]any{"slug": slug, "location": spec.InRepo, "base": base, "commits": commits}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(b))
		return nil
	}
	fmt.Fprintf(out, "%s: repo specs, %s on %s\n", slug, plural(len(commits), "commit"), base)
	for _, c := range commits {
		fmt.Fprintf(out, "%s %s %s\n", c.Commit, c.Date, c.Subject)
	}
	return nil
}

func specShowCmd() *cobra.Command {
	var diff bool
	cmd := &cobra.Command{
		Use:   "show <slug>@<rev> [<capability>]",
		Short: "Print a spec as it was at a version, tag or commit",
		Long: `Prints a spec as it was at <rev>, every file under a "== <file> ==" header
(or only one capability's), after a line naming the version. For a vault
spec <rev> is:
  * a ticket ID with a version note for the slug: that version (its
    specs-after/ snapshot)
  * a tag or a commit, resolved in the main clone: the newest version whose
    merged (or pinned) commit it holds, so @v1.4.0 is the spec as of that
    release; an unpinned version never matches
--diff prints that version's change (requirement changes, then the raw
diff, as ct spec diff) instead. For a repo spec, <rev> is a commit or tag of
the main clone, the tree is its specs/ and --diff its change to specs/.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			arg, rev, ok := strings.Cut(args[0], "@")
			if !ok || arg == "" || rev == "" || strings.HasPrefix(rev, "-") {
				return fmt.Errorf("%q: want <slug>@<ID|tag|commit>", args[0])
			}
			slug, dir, loc, err := specSlug(ctx, arg)
			if err != nil {
				return err
			}
			capability := ""
			if len(args) == 2 {
				capability = strings.Trim(args[1], "/")
			}
			out := cmd.OutOrStdout()
			if loc.Kind == spec.InRepo {
				return repoSpecShow(out, slug, dir, rev, capability, diff)
			}
			lv, err := versionAt(ctx, slug, dir, rev)
			if err != nil {
				return err
			}
			before, after, err := spec.VersionSides(ctx.Vault, slug, lv.Version)
			if err != nil {
				return err
			}
			if capability != "" {
				before, after = spec.UnderCapability(before, capability), spec.UnderCapability(after, capability)
			}
			fmt.Fprintf(out, "%s@%s: version %s (%s)\n", slug, rev, lv.ID, spec.VersionState(lv.Version))
			if diff {
				printSpecDiff(out, slug, spec.InVault, spec.DiffTrees(slug, before, after), false)
				return nil
			}
			printSpecTree(out, after)
			return nil
		},
	}
	cmd.Flags().BoolVar(&diff, "diff", false, "print the version's change instead of the spec")
	return cmd
}

// versionAt picks the version rev names: a ticket ID with a version note,
// else the newest version a tag or commit of the main clone holds.
func versionAt(ctx vault.Context, slug, dir, rev string) (logVersion, error) {
	if note.ValidKey(rev) {
		path := spec.VersionPath(ctx.Vault, slug, rev)
		if _, err := os.Stat(path); err == nil {
			v, err := spec.ReadVersion(path)
			if err != nil {
				return logVersion{}, err
			}
			return logVersion{ID: v.ID(), Note: vaultRel(ctx.Vault, path), Version: v}, nil
		}
	}
	if dir == "" {
		return logVersion{}, fmt.Errorf("no spec version at %s (no version note of that ticket, and no main clone of %s for a tag or commit)", rev, slug)
	}
	target, err := gitx.Out(dir, "rev-parse", "-q", "--verify", rev+"^{commit}")
	if err != nil || target == "" {
		return logVersion{}, fmt.Errorf("no spec version at %s (not a ticket with a version note, a tag or a commit of %s)", rev, slug)
	}
	vs, err := readVersions(ctx, slug, dir)
	if err != nil {
		return logVersion{}, err
	}
	for _, lv := range vs {
		if lv.Commit == "" {
			continue
		}
		if (lv.Merged != "" && spec.IsAncestor(dir, lv.Merged, target)) || spec.IsAncestor(dir, lv.Commit, target) {
			return lv, nil
		}
	}
	return logVersion{}, fmt.Errorf("no spec version at %s", rev)
}

func printSpecTree(out io.Writer, t spec.Tree) {
	for _, f := range t.Keys() {
		fmt.Fprintf(out, "== %s ==\n", f)
		b := t[f]
		out.Write(b)
		if len(b) > 0 && b[len(b)-1] != '\n' {
			fmt.Fprintln(out)
		}
	}
}

func repoSpecShow(out io.Writer, slug, dir, rev, capability string, diff bool) error {
	target, err := gitx.Out(dir, "rev-parse", "-q", "--verify", rev+"^{commit}")
	if err != nil || target == "" {
		if note.ValidKey(rev) {
			return fmt.Errorf("%s: its specs live in the repo, so @%s must be a tag or a commit, not a ticket", slug, rev)
		}
		return fmt.Errorf("%s: %s is not a tag or a commit of the main clone", slug, rev)
	}
	after, err := spec.GitTree(dir, target, "specs")
	if err != nil {
		return err
	}
	before := spec.Tree{}
	if parent, err := gitx.Out(dir, "rev-parse", "-q", "--verify", target+"^"); err == nil && parent != "" {
		if before, err = spec.GitTree(dir, parent, "specs"); err != nil {
			return err
		}
	}
	if capability != "" {
		before, after = spec.UnderCapability(before, capability), spec.UnderCapability(after, capability)
	}
	fmt.Fprintf(out, "%s@%s: repo specs at %s\n", slug, rev, shortSHA(target))
	if diff {
		printSpecDiff(out, slug, spec.InRepo, spec.DiffTrees(slug, before, after), false)
		return nil
	}
	printSpecTree(out, after)
	return nil
}

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/fsx"
	"github.com/zero4573/claude-tickets/internal/links"
	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/spec"
	"github.com/zero4573/claude-tickets/internal/vault"
	"github.com/zero4573/claude-tickets/internal/workspace"
)

// promoteNow is the clock of ct spec promote (a seam for tests).
var promoteNow = time.Now

// The steps of a promote's apply phase and its commit point, one seam each
// (tests swap them to fail a step, then rerun).
var (
	// writeJournal writes the journal: the commit point.
	writeJournal = fsx.WriteFileAtomic
	// renameTree is both renames of a replace-tree.
	renameTree = os.Rename
	// writeStaged is a write of the kept list or the version note.
	writeStaged = fsx.WriteFileAtomic
	// writeSpecFile is a put into projects/.
	writeSpecFile = fsx.WriteFileAtomic
	// trashFile is a trash: a rename into the vault's .trash/.
	trashFile = os.Rename
	// redirectLinks is the link half of a drop.
	redirectLinks = links.RedirectLinks
	// removeFile is the removal half of a drop.
	removeFile = os.Remove
	// appendDone appends to the journal's progress log.
	appendDone = appendFile
	// restoreFile is each write of a hold-back: a snapshot entry, the
	// version note or the .held list put back (remove: deleted).
	restoreFile = restoreWrite
)

// Outcomes of promoting one repo.
const (
	promoteDone    = "promoted"
	promoteNothing = "nothing"
	promoteRefused = "refused"
)

// Ways to resolve a conflict (--resolve <slug>=theirs|ours).
const (
	keepTheirs = "theirs"
	keepOurs   = "ours"
	keepMerged = "merged"
)

type promoteOther struct {
	Ticket   string `json:"ticket"`
	Note     string `json:"note"`
	Promoted string `json:"promoted"`
}

// promoteConflict explains a refusal: the files another ticket changed in
// projects/ since this ticket's snapshots, the version notes that name
// them, both changes from their common ancestor, and the capabilities and
// requirements both changed.
type promoteConflict struct {
	Files   []string       `json:"files"`
	Others  []promoteOther `json:"others"`
	Theirs  spec.Diff      `json:"theirs"`
	Ours    spec.Diff      `json:"ours"`
	Clashes []spec.Clash   `json:"clashes"`
	Copy    string         `json:"copy"`
}

type promoteResolved struct {
	Keep  string   `json:"keep"`
	Files []string `json:"files"`
	// Trash is where --resolve theirs moved the copy's files, vault-relative.
	Trash string `json:"trash,omitempty"`
}

// promoteHeld is a file a replay held back: it changed in projects/ (by
// another ticket or by hand) after this promote planned.
type promoteHeld struct {
	File   string `json:"file"`
	Ticket string `json:"ticket,omitempty"`
	Note   string `json:"note,omitempty"`
	Reason string `json:"reason"`
}

type promoteResult struct {
	Slug         string           `json:"slug"`
	Status       string           `json:"status"`
	Files        []string         `json:"files"`
	Capabilities []string         `json:"capabilities"`
	Version      string           `json:"version,omitempty"`
	Resolved     *promoteResolved `json:"resolved,omitempty"`
	Conflict     *promoteConflict `json:"conflict,omitempty"`
	// Held and Warnings come from a replay of a pending journal.
	Held     []promoteHeld `json:"held,omitempty"`
	Warnings []string      `json:"warnings,omitempty"`
	// Replayed is the pending journal this run finished before its own
	// plan, when that plan had more to do.
	Replayed *promoteResult `json:"replayed,omitempty"`
}

func specPromoteCmd() *cobra.Command {
	var asJSON bool
	var resolve []string
	cmd := &cobra.Command{
		Use:   "promote [--json] [--resolve <slug>=theirs|ours|merged]... <ID>",
		Short: "Move a ticket's vault spec copy into projects/ and record it as a version",
		Long: `For every repo of the ticket's workspace whose specs live in the vault and
whose copy tickets/<source>/<ID>/specs/<slug>/ holds files, one transaction:
  conflict check: a copy file whose projects/<slug>/specs/ version differs
     from this ticket's snapshots (specs-base/<slug>/, taken by ct spec start,
     and specs-after/<slug>/ when there is one) was changed by another ticket
     since. The repo is refused and nothing is written for it; the output
     names the files, the other tickets' version notes that hold them, both
     requirement changes (theirs and ours, from the snapshot) and the
     capabilities and requirements both changed
  plan and stage: from the committed state only, every file the promotion
     writes goes into tickets/<source>/<ID>/specs-stage/<slug>/: the base and
     after snapshots, the kept list, the version note
     projects/<slug>/spec-versions/<slug>-spec-<ID>.md (one per ticket and
     repo; its pin is cleared when the promoted files' content changed) and
     the moved files, their links pointed at projects/
  commit point: the journal specs-stage/<slug>.journal, written last
  apply: base, kept list, the copy's files kept as theirs to .trash/, note,
     after, then each copy file into projects/<slug>/specs/, then (links
     across the vault follow) out of the copy; journal and stage are then
     removed
A run that stops after the commit point is finished by the next ct spec
promote (before any other check of any repo) or ct spec start of the
ticket (which takes the vault lock for it, failing at once when another
session holds it); until then ct spec diff and show refuse the ticket, log
notes it and pin skips it. A stage without a journal is deleted by promote.
The finishing run never overwrites a projects/ file changed (by another
ticket or by hand) after this promote planned: it holds that file back (its snapshot entries
and the version note return to their plan-time state, the copy file
stays), reports it, and the plan that follows refuses it as a conflict,
resolved as below. A copy file edited after the stop stays in the copy,
with a warning.
A refused repo is resolved with --resolve <slug>=theirs (drop the copy's
conflicting files to the vault's .trash/ and keep projects/ as it is; the
files are listed in specs-after/.<slug>.kept, which every later promotion
carries into the after snapshot, and their base entries move onto theirs;
a kept file this ticket never promoted always has theirs as its base),
--resolve <slug>=ours (replace them with the copy), or by merging the
other change into the copy by hand and re-running with --resolve <slug>=merged (the copy is accepted
as the merge: the ticket's before side becomes the current projects/
content of those files). A copy file that already equals projects/ byte for
byte counts as merged without the flag. --json prints the results as data.
Exit 1 when any repo was refused. A copy without the snapshot ct spec start
takes is an error, before anything is written: run ct spec start <ID> <slug>
(it snapshots projects/ as it is then) and promote again. The transaction
and the reasons for it: docs/spec-versions.md, "Promote".`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			out := cmd.OutOrStdout()
			// failFast is an error before the run: under --json it is also
			// printed as the JSON error, with no repos
			failFast := func(err error) error {
				if asJSON {
					b, merr := json.MarshalIndent(map[string]any{"ticket": id, "repos": []promoteResult{}, "error": err.Error()}, "", "  ")
					if merr == nil {
						fmt.Fprintln(out, string(b))
					}
				}
				return err
			}
			ctx, err := vault.Require()
			if err != nil {
				return failFast(err)
			}
			if !note.ValidKey(id) {
				return failFast(fmt.Errorf("invalid ticket ID %q", id))
			}
			info, err := workspace.Read(filepath.Join(ctx.WorkRoot, id))
			if err != nil {
				return failFast(fmt.Errorf("no workspace for %s: %w", id, err))
			}
			res, err := parseResolve(resolve)
			if err != nil {
				return failFast(err)
			}
			var repos []workspace.Repo
			for _, r := range info.Repos {
				loc, err := spec.Resolve(ctx.Vault, r.Slug, r.Path)
				if err != nil {
					return failFast(fmt.Errorf("%s: %w", r.Slug, err))
				}
				if loc.Kind == spec.InVault {
					repos = append(repos, r)
				}
			}
			for slug := range res {
				if !hasSlug(repos, slug) {
					return failFast(fmt.Errorf("--resolve %s: not a repo of %s with its specs in the vault", slug, id))
				}
			}
			results, runErr := runPromote(ctx, id, repos, res)
			if asJSON {
				if results == nil {
					results = []promoteResult{}
				}
				doc := map[string]any{"ticket": id, "repos": results}
				if runErr != nil {
					// an agent reading stdout must not take the run for
					// "nothing to promote"
					doc["error"] = runErr.Error()
				}
				b, err := json.MarshalIndent(doc, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(out, string(b))
			} else {
				for _, pr := range results {
					printPromote(out, id, pr)
				}
			}
			if runErr != nil {
				return runErr
			}
			for _, pr := range results {
				if pr.Status == promoteRefused {
					return exitError(1)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the results as JSON")
	cmd.Flags().StringArrayVar(&resolve, "resolve", nil, "resolve a refused repo: <slug>=theirs (keep projects/), <slug>=ours (keep the copy) or <slug>=merged (the copy is your hand merge); repeatable")
	return cmd
}

func parseResolve(vals []string) (map[string]string, error) {
	out := map[string]string{}
	for _, v := range vals {
		slug, keep, ok := strings.Cut(v, "=")
		if !ok || slug == "" || (keep != keepTheirs && keep != keepOurs && keep != keepMerged) {
			return nil, fmt.Errorf("--resolve %q: want <slug>=theirs, <slug>=ours or <slug>=merged", v)
		}
		out[slug] = keep
	}
	return out, nil
}

func hasSlug(repos []workspace.Repo, slug string) bool {
	for _, r := range repos {
		if r.Slug == slug {
			return true
		}
	}
	return false
}

func vaultRel(vaultDir, p string) string {
	r, err := filepath.Rel(vaultDir, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(r)
}

// runPromote promotes the repos of ticket id (see specPromoteCmd); res is
// the --resolve choices. Every repo's pending journal is replayed first,
// before any other check: a replay error stops the run before any repo is
// planned (the replays already finished stay finished). On an error the
// results of the replays that finished come back with it, so the command
// reports them before failing.
func runPromote(ctx vault.Context, id string, repos []workspace.Repo, res map[string]string) ([]promoteResult, error) {
	replayed := make([]*promoteResult, len(repos))
	for i, r := range repos {
		rp, err := finishPromote(ctx.Vault, id, r.Slug)
		if err != nil {
			return finishedReplays(repos, replayed), fmt.Errorf("%s: %w", r.Slug, err)
		}
		replayed[i] = rp
	}
	for _, r := range repos {
		if err := needsStart(ctx, id, r.Slug); err != nil {
			return finishedReplays(repos, replayed), err
		}
	}
	var results []promoteResult
	for i, r := range repos {
		pr, err := promotePlanned(ctx, id, r, res[r.Slug], replayed[i])
		if err != nil {
			// the repos after this one were not planned: their finished
			// replays still report
			return append(results, finishedReplays(repos[i:], replayed[i:])...), fmt.Errorf("%s: %w", r.Slug, err)
		}
		results = append(results, pr)
	}
	return results, nil
}

// finishedReplays is the results of the repos whose pending journal a run
// replayed to the end, each with nothing planned.
func finishedReplays(repos []workspace.Repo, replayed []*promoteResult) []promoteResult {
	var out []promoteResult
	for i, rp := range replayed {
		if rp != nil {
			out = append(out, promoteResult{Slug: repos[i].Slug, Status: promoteNothing, Files: []string{}, Capabilities: []string{}, Replayed: rp})
		}
	}
	return out
}

// promoteRepo promotes one repo's copy (see specPromoteCmd); keep is the
// --resolve choice for it, "" for none. It is one journaled transaction: a
// pending journal of an earlier run is finished first; then the plan reads
// only committed state, the stage holds every file the promote writes, the
// journal is written (the commit point), applied and removed with the
// stage (docs/spec-versions.md, "Promote").
func promoteRepo(ctx vault.Context, id string, r workspace.Repo, keep string) (promoteResult, error) {
	replayed, err := finishPromote(ctx.Vault, id, r.Slug)
	if err != nil {
		return promoteResult{Slug: r.Slug, Files: []string{}, Capabilities: []string{}}, err
	}
	return promotePlanned(ctx, id, r, keep, replayed)
}

// promotePlanned is promoteRepo once its pending journal (replayed, nil
// for none) is finished.
func promotePlanned(ctx vault.Context, id string, r workspace.Repo, keep string, replayed *promoteResult) (promoteResult, error) {
	pr := promoteResult{Slug: r.Slug, Files: []string{}, Capabilities: []string{}}
	copyDir := spec.CopyDir(ctx.Vault, id, r.Slug)
	cp, err := spec.ReadTree(copyDir)
	if err != nil {
		return pr, err
	}
	if len(cp) == 0 {
		if replayed != nil {
			return *replayed, nil
		}
		pr.Status = promoteNothing
		return pr, nil
	}
	pr.Replayed = replayed
	if err := needsStart(ctx, id, r.Slug); err != nil {
		return pr, err
	}
	p, err := planPromote(ctx, id, r, keep, cp, &pr)
	if err != nil || p == nil {
		return pr, err
	}
	ops, err := stagePromote(ctx.Vault, id, r.Slug, p)
	if err != nil {
		return pr, err
	}
	j, err := applyJournal(ctx.Vault, id, r.Slug, ops)
	if err != nil {
		return pr, err
	}
	// under the vault lock nothing changes projects/ between plan and
	// apply; should it, the result says so
	if len(j.held) > 0 || len(j.warnings) > 0 {
		jr, err := j.result()
		if err != nil {
			return pr, err
		}
		pr.Held, pr.Warnings = jr.Held, jr.Warnings
	}
	return pr, cleanPromote(ctx.Vault, id, r.Slug)
}

// promotePlan is what a promote writes: the new base and after snapshots
// (nil: left as they are), kept list (nil: left as it is) and version note
// (nil: none), the projects/ files with their new content (moves) and the
// copy's files that go to .trash/. A replay reads the rest: the plan-time
// snapshots and note (prev*, nil: none), the projects/ hash of each moved
// file (pre) and of each copy file (copyHash).
type promotePlan struct {
	base, after         spec.Tree
	kept                []string
	note                []byte
	moves               spec.Tree
	trash               []string
	prevBase, prevAfter spec.Tree
	prevNote            []byte
	pre, copyHash       map[string]string
}

// planPromote reads the committed state and works out the promote of the
// copy cp, filling pr with its outcome. A nil plan writes nothing (a
// refused repo).
func planPromote(ctx vault.Context, id string, r workspace.Repo, keep string, cp spec.Tree, pr *promoteResult) (*promotePlan, error) {
	dir := spec.VaultDir(ctx.Vault, r.Slug)
	baseDir, afterDir := spec.BaseDir(ctx.Vault, id, r.Slug), spec.AfterDir(ctx.Vault, id, r.Slug)
	notePath := spec.VersionPath(ctx.Vault, r.Slug, id)
	projects, err := spec.ReadTree(dir)
	if err != nil {
		return nil, err
	}
	base, err := spec.ReadSnapshot(baseDir)
	if err != nil {
		return nil, err
	}
	oldAfter, err := spec.ReadSnapshot(afterDir)
	if err != nil {
		return nil, err
	}
	v, err := spec.ReadVersion(notePath)
	hasNote := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	oldKept, err := spec.ReadKept(spec.KeptList(ctx.Vault, id, r.Slug))
	if err != nil {
		return nil, err
	}
	for _, f := range cp.Keys() {
		if !spec.PlainName(f) {
			return nil, fmt.Errorf("%s: copy file %q: a name with a control character can't be promoted; rename it", r.Slug, f)
		}
	}
	if err := refuseUnfilled(ctx, id, r.Slug, oldAfter, projects, v, cp); err != nil {
		return nil, err
	}

	// A file kept as theirs that this ticket never promoted holds no change
	// of this ticket's: its base is theirs (the after snapshot's entry,
	// else projects/), so a change made on top of theirs is neither refused
	// nor credited with the other ticket's change. Once the ticket promotes
	// the file, its base stays.
	newBase := spec.Overlay(spec.Tree{}, base)
	for _, f := range oldKept {
		if v.HasFile(f) {
			continue
		}
		t, ok := oldAfter[f]
		if oldAfter == nil || !ok {
			t, ok = projects[f]
		}
		setEntry(newBase, f, t, ok)
	}

	conf := spec.Conflicts(projects, newBase, oldAfter, cp)
	// a copy file that already holds theirs is a finished merge
	var merged, open []string
	for _, f := range conf {
		if t, ok := projects[f]; ok && bytes.Equal(t, cp[f]) {
			merged = append(merged, f)
		} else {
			open = append(open, f)
		}
	}
	p := &promotePlan{prevBase: base, prevAfter: oldAfter, pre: map[string]string{}, copyHash: map[string]string{}}
	for f, b := range cp {
		p.copyHash[f] = spec.Hash(b)
	}
	if hasNote {
		if p.prevNote, err = os.ReadFile(notePath); err != nil {
			return nil, err
		}
	}
	kept := oldKept
	if len(open) > 0 {
		switch keep {
		case "":
			c, err := explainConflict(ctx, id, r.Slug, open, projects, newBase, oldAfter, cp)
			if err != nil {
				return nil, err
			}
			pr.Status, pr.Conflict = promoteRefused, c
			return nil, nil
		case keepTheirs:
			// the copy's files may hold work worth keeping: they go to the
			// vault's .trash/, never away, and are listed as kept
			kept = spec.MergeFiles(kept, open)
			p.kept = kept
			p.trash = open
			cp = spec.Overlay(spec.Tree{}, cp)
			for _, f := range open {
				delete(cp, f)
			}
			pr.Resolved = &promoteResolved{Keep: keep, Files: open, Trash: vaultRel(ctx.Vault, promoteTrash(ctx.Vault, id, r.Slug)) + "/"}
		case keepMerged:
			merged = append(merged, open...)
			pr.Resolved = &promoteResolved{Keep: keep, Files: open}
		default:
			pr.Resolved = &promoteResolved{Keep: keep, Files: open}
		}
	}
	// The base entries move onto theirs: for a merge the ticket's change
	// reads as the merge on top of theirs, and for files kept as theirs the
	// ticket changed nothing in them.
	rebase := merged
	if keep == keepTheirs {
		rebase = append(append([]string(nil), merged...), open...)
	}
	for _, f := range rebase {
		t, ok := projects[f]
		setEntry(newBase, f, t, ok)
	}
	if !sameTree(newBase, base) {
		p.base = newBase
	}
	earlier := spec.MergeFiles(v.Files, kept)
	if len(cp) == 0 {
		// everything was theirs: only the after snapshot moves on, by the
		// files kept as theirs, so the kept change isn't refused again
		p.after = spec.TicketAfter(oldAfter, projects, earlier, spec.Pick(projects, open))
		pr.Status = promoteNothing
		return p, nil
	}

	// The moved files get their links rewritten as a move would: a link
	// naming a copy file names its projects/ path.
	moved := cp.Keys()
	rs := make([]links.Redirect, len(moved))
	for i, f := range moved {
		rs[i] = links.Redirect{Src: vaultRel(ctx.Vault, filepath.Join(spec.CopyDir(ctx.Vault, id, r.Slug), filepath.FromSlash(f))), Dst: vaultRel(ctx.Vault, filepath.Join(dir, filepath.FromSlash(f)))}
	}
	p.moves = spec.Tree{}
	for _, f := range moved {
		b := cp[f]
		if strings.HasSuffix(f, ".md") {
			s, _ := links.RewriteText(filepath.Join(spec.CopyDir(ctx.Vault, id, r.Slug), filepath.FromSlash(f)), string(b), rs)
			b = []byte(s)
		}
		p.moves[f] = b
		p.pre[f] = spec.PreAbsent
		if t, ok := projects[f]; ok {
			p.pre[f] = spec.Hash(t)
		}
	}
	// The after snapshot holds this ticket's files only: a file promoted
	// earlier and changed since by another ticket keeps this ticket's
	// content. The files kept as theirs, now and by earlier promotions,
	// go into it too, under the moved ones.
	now := spec.Overlay(projects, p.moves)
	p.after = spec.TicketAfter(oldAfter, now, earlier, spec.Pick(now, append(append([]string{}, open...), moved...)))
	if !hasNote {
		v = spec.Version{Slug: r.Slug, Ticket: "[[" + id + "]]", Project: "[[" + r.Slug + "]]"}
	}
	v.Files = spec.MergeFiles(v.Files, moved)
	if oldAfter != nil && spec.PinStale(oldAfter, p.after, v.Files) {
		v.ClearPin()
	}
	v.Branch = r.Branch
	v.Promoted = promoteNow().Format(spec.TimeLayout)
	before, aft := spec.Sides(p.after, newBase, p.after, spec.Tree{}, v.Files)
	p.note, err = spec.RenderVersion(v, note.RelDir(ctx.Vault, id), before, spec.DiffTrees(r.Slug, before, aft))
	if err != nil {
		return nil, err
	}
	pr.Status, pr.Files, pr.Version = promoteDone, moved, vaultRel(ctx.Vault, notePath)
	pr.Capabilities = capsOf(moved)
	return p, nil
}

func setEntry(t spec.Tree, f string, b []byte, ok bool) {
	if ok {
		t[f] = b
	} else {
		delete(t, f)
	}
}

func sameTree(a, b spec.Tree) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for f, x := range a {
		if y, ok := b[f]; !ok || !bytes.Equal(x, y) {
			return false
		}
	}
	return true
}

// promoteTrash is where --resolve theirs moves the copy's files.
func promoteTrash(vaultDir, id, slug string) string {
	return filepath.Join(vaultDir, ".trash", id+"-spec-theirs", slug)
}

// stagePromote writes the plan into the stage and then the journal, the
// commit point; it returns the journal's operations. A failure before the
// journal leaves an incomplete stage, which the next run deletes.
func stagePromote(vaultDir, id, slug string, p *promotePlan) ([]spec.Op, error) {
	stage := spec.StageDir(vaultDir, id, slug)
	if err := removeStage(vaultDir, id, slug); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return nil, err
	}
	var ops []spec.Op
	if p.base != nil {
		if err := spec.WriteTree(filepath.Join(stage, spec.TreeBase), p.base); err != nil {
			return nil, err
		}
		ops = append(ops, spec.Op{Kind: spec.OpReplaceTree, Arg: spec.TreeBase})
	}
	if p.kept != nil {
		if err := spec.WriteKept(filepath.Join(stage, spec.WriteOfKept), p.kept); err != nil {
			return nil, err
		}
		ops = append(ops, spec.Op{Kind: spec.OpWrite, Arg: spec.WriteOfKept, Dest: vaultRel(vaultDir, spec.KeptList(vaultDir, id, slug))})
	}
	for _, f := range p.trash {
		ops = append(ops, spec.Op{Kind: spec.OpTrash, Arg: f, Hash: p.copyHash[f]})
	}
	if p.note != nil {
		if err := fsx.WriteFileAtomic(filepath.Join(stage, "note.md"), p.note, 0o644); err != nil {
			return nil, err
		}
		ops = append(ops, spec.Op{Kind: spec.OpWrite, Arg: spec.WriteOfNote, Dest: vaultRel(vaultDir, spec.VersionPath(vaultDir, slug, id))})
	}
	if p.after != nil {
		if err := spec.WriteTree(filepath.Join(stage, spec.TreeAfter), p.after); err != nil {
			return nil, err
		}
		ops = append(ops, spec.Op{Kind: spec.OpReplaceTree, Arg: spec.TreeAfter})
	}
	if len(p.moves) > 0 {
		if err := spec.WriteTree(filepath.Join(stage, "moves"), p.moves); err != nil {
			return nil, err
		}
		// prev/: what a hold-back puts back, each put file's plan-time
		// snapshot entries and the plan-time note
		files := p.moves.Keys()
		prevBase, prevAfter := spec.Tree{}, spec.Tree{}
		var absBase, absAfter []string
		for _, f := range files {
			if b, ok := p.prevBase[f]; ok {
				prevBase[f] = b
			} else {
				absBase = append(absBase, f)
			}
			if b, ok := p.prevAfter[f]; ok {
				prevAfter[f] = b
			} else {
				absAfter = append(absAfter, f)
			}
		}
		prev := filepath.Join(stage, "prev")
		if err := spec.WriteTree(filepath.Join(prev, spec.TreeBase), prevBase); err != nil {
			return nil, err
		}
		if err := spec.WriteTree(filepath.Join(prev, spec.TreeAfter), prevAfter); err != nil {
			return nil, err
		}
		absNote := ""
		if p.prevNote != nil {
			if err := fsx.WriteFileAtomic(filepath.Join(prev, "note.md"), p.prevNote, 0o644); err != nil {
				return nil, err
			}
		} else {
			absNote = vaultRel(vaultDir, spec.VersionPath(vaultDir, slug, id))
		}
		if err := fsx.WriteFileAtomic(filepath.Join(prev, spec.PrevAbsent), spec.RenderAbsent(absBase, absAfter, absNote), 0o644); err != nil {
			return nil, err
		}
		for _, f := range files {
			ops = append(ops, spec.Op{Kind: spec.OpPut, Arg: f, Hash: p.pre[f]})
		}
		for _, f := range files {
			ops = append(ops, spec.Op{Kind: spec.OpDrop, Arg: f, Hash: p.copyHash[f]})
		}
	}
	if err := writeJournal(spec.JournalPath(vaultDir, id, slug), spec.RenderJournal(ops), 0o644); err != nil {
		return nil, err
	}
	return ops, nil
}

// removeStage removes a stage with no journal: its folder, progress log
// and held list.
func removeStage(vaultDir, id, slug string) error {
	for _, p := range []string{spec.DonePath(vaultDir, id, slug), spec.HeldPath(vaultDir, id, slug)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.RemoveAll(spec.StageDir(vaultDir, id, slug))
}

// finishPromote finishes ticket id's pending promote of slug (its journal)
// and returns what it promoted and held back; nil when there was none. A
// stage left without a journal (a run that stopped before its commit
// point) is deleted.
func finishPromote(vaultDir, id, slug string) (*promoteResult, error) {
	ops, ok, err := spec.ReadJournal(vaultDir, id, slug)
	if err != nil {
		return nil, err
	}
	if !ok {
		if err := removeStage(vaultDir, id, slug); err != nil {
			return nil, err
		}
		_ = os.Remove(spec.StageRoot(vaultDir, id))
		return nil, nil
	}
	j, err := applyJournal(vaultDir, id, slug, ops)
	if err != nil {
		return nil, err
	}
	pr, err := j.result()
	if err != nil {
		return nil, err
	}
	if err := cleanPromote(vaultDir, id, slug); err != nil {
		return nil, err
	}
	return &pr, nil
}

// journalRun is one application of a promote journal: its operations,
// those its progress log lists as done, the files held back and the
// warnings given.
type journalRun struct {
	vault, id, slug     string
	stage, copyDir, dir string
	ops                 []spec.Op
	done                map[int]bool
	// complete is the length of the progress log's whole lines; torn: a
	// last line without its newline follows, cut off before the next append
	complete int
	torn     bool
	held     map[string]bool
	absent   map[string]map[string]bool
	warnings []string
}

func (j *journalRun) copyFile(f string) string {
	return filepath.Join(j.copyDir, filepath.FromSlash(f))
}

func (j *journalRun) dest(f string) string { return filepath.Join(j.dir, filepath.FromSlash(f)) }

func (j *journalRun) staged(parts ...string) string {
	return filepath.Join(append([]string{j.stage}, parts...)...)
}

// treeDir is where tree (base or after) is restored and read: its staged
// folder while that is there (its replace-tree hasn't renamed it in), else
// the live snapshot.
func (j *journalRun) treeDir(tree string) string {
	if st, err := os.Lstat(j.staged(tree)); err == nil && st.IsDir() {
		return j.staged(tree)
	}
	if tree == spec.TreeBase {
		return spec.BaseDir(j.vault, j.id, j.slug)
	}
	return spec.AfterDir(j.vault, j.id, j.slug)
}

func exists(p string) (bool, error) {
	_, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// applyJournal applies a journal's operations (docs/spec-versions.md,
// "Promote"): those its progress log lists are
// skipped; before any is applied, a put whose projects/ file is neither
// its staged content nor its plan-time content is held back (holdBack);
// the rest run in order, each idempotent, and each is marked done once it
// completes.
func applyJournal(vaultDir, id, slug string, ops []spec.Op) (*journalRun, error) {
	j := &journalRun{
		vault: vaultDir, id: id, slug: slug, ops: ops,
		stage:   spec.StageDir(vaultDir, id, slug),
		copyDir: spec.CopyDir(vaultDir, id, slug),
		dir:     spec.VaultDir(vaultDir, slug),
		held:    map[string]bool{},
	}
	damaged := func(err error) error { return spec.DamagedJournal(vaultDir, id, slug, err) }
	read := func(p string) ([]byte, error) {
		b, err := os.ReadFile(p)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return b, err
	}
	b, err := read(spec.DonePath(vaultDir, id, slug))
	if err != nil {
		return nil, err
	}
	if j.done, j.complete, err = spec.ParseDone(b, len(ops)); err != nil {
		return nil, damaged(err)
	}
	j.torn = j.complete < len(b)
	if b, err = read(spec.HeldPath(vaultDir, id, slug)); err != nil {
		return nil, err
	}
	held, err := spec.ParseHeld(b)
	if err != nil {
		return nil, damaged(err)
	}
	for _, f := range held {
		j.held[f] = true
	}
	if b, err = read(j.staged("prev", spec.PrevAbsent)); err != nil {
		return nil, err
	}
	if j.absent, err = spec.ParseAbsent(b); err != nil {
		return nil, damaged(err)
	}

	// pre-flight: which pending puts another ticket's promotion overtook
	var hold []string
	for i, o := range ops {
		if o.Kind != spec.OpPut || j.done[i+1] || j.held[o.Arg] {
			continue
		}
		staged, err := os.ReadFile(j.staged("moves", filepath.FromSlash(o.Arg)))
		if errors.Is(err, os.ErrNotExist) {
			return nil, damaged(fmt.Errorf("the stage lacks moves/%s", o.Arg))
		} else if err != nil {
			return nil, err
		}
		cur, err := os.ReadFile(j.dest(o.Arg))
		switch {
		case err == nil && (bytes.Equal(cur, staged) || spec.Hash(cur) == o.Hash):
		case errors.Is(err, os.ErrNotExist) && o.Hash == spec.PreAbsent:
		case err != nil && !errors.Is(err, os.ErrNotExist):
			return nil, err
		default:
			hold = append(hold, o.Arg)
		}
	}
	if err := j.checkStage(hold); err != nil {
		return nil, damaged(err)
	}
	if len(hold) > 0 {
		if err := j.holdBack(hold); err != nil {
			return nil, err
		}
	}

	redirected := false
	for i, o := range ops {
		if j.done[i+1] || ((o.Kind == spec.OpPut || o.Kind == spec.OpDrop) && j.held[o.Arg]) {
			continue
		}
		switch o.Kind {
		case spec.OpReplaceTree:
			dst := spec.BaseDir(vaultDir, id, slug)
			if o.Arg == spec.TreeAfter {
				dst = spec.AfterDir(vaultDir, id, slug)
			}
			if err := replaceTree(j.staged(o.Arg), dst); err != nil {
				return nil, err
			}
		case spec.OpWrite:
			dst, src := spec.KeptList(vaultDir, id, slug), j.staged(spec.WriteOfKept)
			if o.Arg == spec.WriteOfNote {
				dst, src = spec.VersionPath(vaultDir, slug, id), j.staged("note.md")
			}
			if want := vaultRel(vaultDir, dst); o.Dest != want {
				return nil, damaged(fmt.Errorf("%s, want %s", o, want))
			}
			b, err := os.ReadFile(src)
			if errors.Is(err, os.ErrNotExist) && o.Arg == spec.WriteOfNote {
				// a hold-back left the promote no file and there was no
				// note before it: there is none
				if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
					return nil, err
				}
				break
			} else if err != nil {
				return nil, err
			}
			if cur, err := os.ReadFile(dst); err == nil && bytes.Equal(cur, b) {
				break
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return nil, err
			}
			if err := writeStaged(dst, b, 0o644); err != nil {
				return nil, err
			}
		case spec.OpPut:
			b, err := os.ReadFile(j.staged("moves", filepath.FromSlash(o.Arg)))
			if err != nil {
				return nil, err
			}
			dst := j.dest(o.Arg)
			if cur, err := os.ReadFile(dst); err == nil && bytes.Equal(cur, b) {
				break
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return nil, err
			}
			if err := writeSpecFile(dst, b, 0o644); err != nil {
				return nil, err
			}
		case spec.OpTrash:
			src := j.copyFile(o.Arg)
			if ok, err := j.unchanged(o); err != nil || !ok {
				if err != nil {
					return nil, err
				}
				break
			}
			dst := uniqueDest(filepath.Join(promoteTrash(vaultDir, id, slug), filepath.FromSlash(o.Arg)))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return nil, err
			}
			if err := trashFile(src, dst); err != nil {
				return nil, err
			}
		case spec.OpDrop:
			if !redirected {
				if err := j.redirect(i); err != nil {
					return nil, err
				}
				redirected = true
			}
			if ok, err := j.unchanged(o); err != nil || !ok {
				if err != nil {
					return nil, err
				}
				break
			}
			if err := removeFile(j.copyFile(o.Arg)); err != nil {
				return nil, err
			}
		}
		if err := j.mark(i + 1); err != nil {
			return nil, err
		}
	}
	return j, nil
}

// sameCopy reports whether copy file content b is as planned: its
// plan-time hash, or (a drop's file, whose links into the other moved files
// the redirect already rewrote) its staged move.
func (j *journalRun) sameCopy(o spec.Op, b []byte) bool {
	if spec.Hash(b) == o.Hash {
		return true
	}
	if o.Kind != spec.OpDrop {
		return false
	}
	m, err := os.ReadFile(j.staged("moves", filepath.FromSlash(o.Arg)))
	if err != nil {
		return false
	}
	if bytes.Equal(m, b) {
		return true
	}
	// A held-back file's drop doesn't run, so the redirect leaves the
	// links to its copy as they were, in whatever form they were written:
	// pointing them at projects/ as the plan did, through the same
	// rewrite, gives the staged move.
	if !strings.HasSuffix(o.Arg, ".md") {
		return false
	}
	var fwd []links.Redirect
	for _, h := range j.ops {
		if h.Kind == spec.OpDrop && j.held[h.Arg] {
			fwd = append(fwd, links.Redirect{Src: vaultRel(j.vault, j.copyFile(h.Arg)), Dst: vaultRel(j.vault, j.dest(h.Arg))})
		}
	}
	if len(fwd) == 0 {
		return false
	}
	s, _ := links.RewriteText(j.copyFile(o.Arg), string(b), fwd)
	return s == string(m)
}

// unchanged reports whether a trash or drop applies: the copy file is
// there as planned (sameCopy). A copy file edited since is left, with a
// warning; one already gone is skipped.
func (j *journalRun) unchanged(o spec.Op) (bool, error) {
	b, err := os.ReadFile(j.copyFile(o.Arg))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if !j.sameCopy(o, b) {
		j.warnings = append(j.warnings, fmt.Sprintf("%s changed in the copy after this promote planned: left there, it promotes later as an edit", o.Arg))
		return false, nil
	}
	return true, nil
}

// redirect points the links naming the copy files of the drops from op
// index from on at projects/, in one pass over the vault: every drop that
// will run (not done, not held back, its copy file there with its hash).
func (j *journalRun) redirect(from int) error {
	var rs []links.Redirect
	for k := from; k < len(j.ops); k++ {
		o := j.ops[k]
		if o.Kind != spec.OpDrop || j.done[k+1] || j.held[o.Arg] {
			continue
		}
		b, err := os.ReadFile(j.copyFile(o.Arg))
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if !j.sameCopy(o, b) {
			continue
		}
		rs = append(rs, links.Redirect{Src: vaultRel(j.vault, j.copyFile(o.Arg)), Dst: vaultRel(j.vault, j.dest(o.Arg))})
	}
	if len(rs) == 0 {
		return nil
	}
	_, err := redirectLinks(j.vault, rs)
	return err
}

// mark appends the line numbers ns to the progress log in one write,
// cutting off a torn last line first.
func (j *journalRun) mark(ns ...int) error {
	p := spec.DonePath(j.vault, j.id, j.slug)
	if j.torn {
		if err := os.Truncate(p, int64(j.complete)); err != nil {
			return err
		}
		j.torn = false
	}
	var b strings.Builder
	for _, n := range ns {
		fmt.Fprintf(&b, "%d\n", n)
	}
	if err := appendDone(p, []byte(b.String())); err != nil {
		return err
	}
	for _, n := range ns {
		j.done[n] = true
	}
	return nil
}

func appendFile(p string, b []byte) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func restoreWrite(p string, b []byte, remove bool) error {
	if remove {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return fsx.WriteFileAtomic(p, b, 0o644)
}

// checkStage refuses a stage that lacks a file the pending operations
// read (hold: the puts the pre-flight holds back). The pending puts'
// moves/ files were read by the pre-flight. The prev/ entries of every put
// are checked, done ones too: that is stricter than the files the pending
// operations read, so a stage whose prev/ was partly cleaned after its
// puts were marked is refused as damaged, not replayed on a guess.
func (j *journalRun) checkStage(hold []string) error {
	anyHeld := len(j.held)+len(hold) > 0
	noteRel := vaultRel(j.vault, spec.VersionPath(j.vault, j.slug, j.id))
	puts := false
	for i, o := range j.ops {
		switch o.Kind {
		case spec.OpWrite:
			if j.done[i+1] {
				continue
			}
			src := j.staged(spec.WriteOfKept)
			if o.Arg == spec.WriteOfNote {
				src = j.staged("note.md")
			}
			ok, err := exists(src)
			if err != nil {
				return err
			}
			// a hold-back removes a note that would list no file and
			// had no plan-time version
			if !ok && !(o.Arg == spec.WriteOfNote && anyHeld && j.absent[spec.WriteOfNote][o.Dest]) {
				return fmt.Errorf("the stage lacks %s", vaultRel(j.vault, src))
			}
		case spec.OpPut:
			puts = true
			for _, tree := range []string{spec.TreeBase, spec.TreeAfter} {
				ok, err := exists(j.staged("prev", tree, filepath.FromSlash(o.Arg)))
				if err != nil {
					return err
				}
				if ok == j.absent[tree][o.Arg] {
					return fmt.Errorf("prev/%s/%s: in both prev/ and prev/absent, or in neither", tree, o.Arg)
				}
			}
		}
	}
	if puts {
		ok, err := exists(j.staged("prev", "note.md"))
		if err != nil {
			return err
		}
		if ok == j.absent[spec.WriteOfNote][noteRel] {
			return errors.New("prev/note.md: in both prev/ and prev/absent, or in neither")
		}
	}
	return nil
}

// holdBack holds back the files hold: their puts and drops are skipped,
// their snapshot entries return to their plan-time state (in the staged
// tree while there is one, else the live one) and the version note is
// rendered again without them; the copy files stay. Then they are listed
// in .held and their puts and drops marked done. Every step is redone
// whole by a rerun until then.
func (j *journalRun) holdBack(hold []string) error {
	for _, f := range hold {
		for _, tree := range []string{spec.TreeBase, spec.TreeAfter} {
			dst := filepath.Join(j.treeDir(tree), filepath.FromSlash(f))
			if j.absent[tree][f] {
				if err := restoreFile(dst, nil, true); err != nil {
					return err
				}
				continue
			}
			b, err := os.ReadFile(j.staged("prev", tree, filepath.FromSlash(f)))
			if err != nil {
				return err
			}
			if err := restoreFile(dst, b, false); err != nil {
				return err
			}
		}
	}
	for _, f := range hold {
		j.held[f] = true
	}
	if err := j.renderNote(); err != nil {
		return err
	}
	files := make([]string, 0, len(j.held))
	for f := range j.held {
		files = append(files, f)
	}
	sort.Strings(files)
	if err := restoreFile(spec.HeldPath(j.vault, j.id, j.slug), []byte(strings.Join(files, "\n")+"\n"), false); err != nil {
		return err
	}
	var ns []int
	for i, o := range j.ops {
		if (o.Kind == spec.OpPut || o.Kind == spec.OpDrop) && slices.Contains(hold, o.Arg) && !j.done[i+1] {
			ns = append(ns, i+1)
		}
	}
	return j.mark(ns...)
}

// renderNote renders the staged version note again as planning does, for
// the files not held back: its files are the plan-time note's and the
// moved ones, its diff comes from the snapshots as restored. A note left
// with no file and no plan-time version is removed instead. A note already
// written is written again.
func (j *journalRun) renderNote() error {
	noteN := 0
	var noteOp spec.Op
	for i, o := range j.ops {
		if o.Kind == spec.OpWrite && o.Arg == spec.WriteOfNote {
			noteN, noteOp = i+1, o
		}
	}
	if noteN == 0 {
		return nil
	}
	staged := j.staged("note.md")
	v, err := spec.ReadVersion(staged)
	if errors.Is(err, os.ErrNotExist) {
		return nil // removed by an earlier hold-back: still no file
	} else if err != nil {
		return err
	}
	var files []string
	noPrev := j.absent[spec.WriteOfNote][noteOp.Dest]
	if !noPrev {
		pv, err := spec.ReadVersion(j.staged("prev", "note.md"))
		if err != nil {
			return err
		}
		files = pv.Files
	}
	var moved []string
	for _, o := range j.ops {
		if o.Kind == spec.OpPut && !j.held[o.Arg] {
			moved = append(moved, o.Arg)
		}
	}
	v.Files = spec.MergeFiles(files, moved)
	live := spec.VersionPath(j.vault, j.slug, j.id)
	if len(v.Files) == 0 && noPrev {
		if err := restoreFile(staged, nil, true); err != nil {
			return err
		}
		if j.done[noteN] {
			return restoreFile(live, nil, true)
		}
		return nil
	}
	base, err := spec.ReadSnapshot(j.treeDir(spec.TreeBase))
	if err != nil {
		return err
	}
	after, err := spec.ReadSnapshot(j.treeDir(spec.TreeAfter))
	if err != nil {
		return err
	}
	if after == nil {
		after = spec.Tree{}
	}
	before, aft := spec.Sides(after, base, after, spec.Tree{}, v.Files)
	body, err := spec.RenderVersion(v, note.RelDir(j.vault, j.id), before, spec.DiffTrees(j.slug, before, aft))
	if err != nil {
		return err
	}
	if err := restoreFile(staged, body, false); err != nil {
		return err
	}
	if j.done[noteN] {
		return restoreFile(live, body, false)
	}
	return nil
}

// result is what the journal promoted, resolved and held back. A held-back
// file names the newest other ticket whose after copy of it is what
// projects/ holds now (spec.ChangedBy), else no one.
func (j *journalRun) result() (promoteResult, error) {
	pr := promoteResult{Slug: j.slug, Status: promoteNothing, Files: []string{}, Capabilities: []string{}, Warnings: j.warnings}
	var trashed []string
	for _, o := range j.ops {
		switch o.Kind {
		case spec.OpPut:
			if !j.held[o.Arg] {
				pr.Files = append(pr.Files, o.Arg)
			}
		case spec.OpTrash:
			trashed = append(trashed, o.Arg)
		case spec.OpWrite:
			if o.Arg == spec.WriteOfNote {
				if ok, err := exists(spec.VersionPath(j.vault, j.slug, j.id)); err != nil {
					return pr, err
				} else if ok {
					pr.Version = o.Dest
				}
			}
		}
	}
	if len(pr.Files) > 0 {
		pr.Status, pr.Capabilities = promoteDone, capsOf(pr.Files)
	}
	if len(trashed) > 0 {
		pr.Resolved = &promoteResolved{Keep: keepTheirs, Files: trashed, Trash: vaultRel(j.vault, promoteTrash(j.vault, j.id, j.slug)) + "/"}
	}
	if len(j.held) == 0 {
		return pr, nil
	}
	versions, err := spec.ListVersions(j.vault, j.slug)
	if err != nil {
		return pr, err
	}
	files := make([]string, 0, len(j.held))
	for f := range j.held {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		h := promoteHeld{File: f, Reason: "projects/ changed after this promote planned; no version note holds it"}
		cur, ok, err := readOptional(j.dest(f))
		if err != nil {
			return pr, err
		}
		by, err := spec.ChangedBy(j.vault, j.slug, versions, j.id, f, cur, ok)
		if err != nil {
			return pr, err
		}
		if len(by) > 0 {
			h.Ticket, h.Note = by[0].Version.ID(), vaultRel(j.vault, by[0].Path)
			h.Reason = fmt.Sprintf("projects/ holds %s's recorded after copy (%s)", h.Ticket, h.Note)
		}
		pr.Held = append(pr.Held, h)
	}
	return pr, nil
}

// readOptional reads p; ok is false when it doesn't exist.
func readOptional(p string) (b []byte, ok bool, err error) {
	b, err = os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

// replaceTree renames the staged folder over dst, the old dst set aside
// (.<name>.old-*, as spec.WriteTree names it) until the new one is in. A
// staged folder already gone was renamed in: only leftovers are removed.
func replaceTree(staged, dst string) error {
	parent, name := filepath.Dir(dst), filepath.Base(dst)
	olds, _ := filepath.Glob(filepath.Join(parent, "."+name+".old-*"))
	if _, err := os.Lstat(staged); errors.Is(err, os.ErrNotExist) {
		for _, o := range olds {
			if err := os.RemoveAll(o); err != nil {
				return err
			}
		}
		return nil
	} else if err != nil {
		return err
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		old := filepath.Join(parent, fmt.Sprintf(".%s.old-%d", name, time.Now().UnixNano()))
		if err := renameTree(dst, old); err != nil {
			return err
		}
		olds = append(olds, old)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := renameTree(staged, dst); err != nil {
		return err
	}
	for _, o := range olds {
		if err := os.RemoveAll(o); err != nil {
			return err
		}
	}
	return nil
}

// cleanPromote removes an applied journal, then its progress log, held
// list and stage, and the empty folders left in the copy.
func cleanPromote(vaultDir, id, slug string) error {
	if err := os.Remove(spec.JournalPath(vaultDir, id, slug)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := removeStage(vaultDir, id, slug); err != nil {
		return err
	}
	_ = os.Remove(spec.StageRoot(vaultDir, id))
	fsx.RemoveEmptyDirs(spec.CopyDir(vaultDir, id, slug))
	return nil
}

func capsOf(files []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, f := range files {
		c := strings.SplitN(f, "/", 2)[0]
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// explainConflict describes a refusal of the conflicting files conf. Its
// others are the tickets whose after copy of one of the files is what
// projects/ holds now (spec.ChangedBy), newest first.
func explainConflict(ctx vault.Context, id, slug string, conf []string, projects, base, after, cp spec.Tree) (*promoteConflict, error) {
	anc := spec.Ancestor(base, after, conf)
	c := &promoteConflict{
		Files:  conf,
		Others: []promoteOther{},
		Theirs: spec.DiffTrees(slug, anc, spec.Pick(projects, conf)),
		Ours:   spec.DiffTrees(slug, anc, spec.Pick(cp, conf)),
		Copy:   vaultRel(ctx.Vault, spec.CopyDir(ctx.Vault, id, slug)) + "/",
	}
	c.Clashes = spec.Clashes(c.Theirs, c.Ours)
	if c.Clashes == nil {
		c.Clashes = []spec.Clash{}
	}
	versions, err := spec.ListVersions(ctx.Vault, slug)
	if err != nil {
		return nil, err
	}
	named := map[string]bool{}
	for _, f := range conf {
		cur, ok := projects[f]
		by, err := spec.ChangedBy(ctx.Vault, slug, versions, id, f, cur, ok)
		if err != nil {
			return nil, err
		}
		for _, vf := range by {
			if !named[vf.Path] {
				named[vf.Path] = true
				c.Others = append(c.Others, promoteOther{Ticket: vf.Version.ID(), Note: vaultRel(ctx.Vault, vf.Path), Promoted: vf.Version.Promoted})
			}
		}
	}
	// newest first across the files, as spec.ListVersions orders the notes
	sort.SliceStable(c.Others, func(a, b int) bool { return c.Others[a].Promoted > c.Others[b].Promoted })
	return c, nil
}

func printPromote(out io.Writer, id string, pr promoteResult) {
	if pr.Replayed != nil {
		fmt.Fprintf(out, "%s: finished the pending promote first:\n", pr.Slug)
		printPromote(out, id, *pr.Replayed)
	}
	for _, w := range pr.Warnings {
		fmt.Fprintf(out, "%s: warning: %s\n", pr.Slug, w)
	}
	for _, h := range pr.Held {
		fmt.Fprintf(out, "%s: held back %s: %s\n", pr.Slug, h.File, h.Reason)
	}
	if pr.Resolved != nil {
		fmt.Fprintf(out, "%s: kept %s for %s\n", pr.Slug, pr.Resolved.Keep, strings.Join(pr.Resolved.Files, ", "))
		if pr.Resolved.Trash != "" {
			fmt.Fprintf(out, "%s: the copy's files moved to %s\n", pr.Slug, pr.Resolved.Trash)
		}
	}
	switch pr.Status {
	case promoteNothing:
		fmt.Fprintf(out, "%s: nothing to promote\n", pr.Slug)
	case promoteDone:
		fmt.Fprintf(out, "%s: promoted %d files (%s); version %s\n", pr.Slug, len(pr.Files), strings.Join(pr.Capabilities, ", "), pr.Version)
	case promoteRefused:
		c := pr.Conflict
		fmt.Fprintf(out, "%s: refused: another ticket changed %s in projects/%s/specs/ since this ticket's snapshot\n", pr.Slug, strings.Join(c.Files, ", "), pr.Slug)
		if len(c.Others) == 0 {
			fmt.Fprintln(out, "  changed by: no version note names these files")
		}
		for _, o := range c.Others {
			fmt.Fprintf(out, "  changed by: %s (%s, promoted %s)\n", o.Ticket, o.Note, o.Promoted)
		}
		for _, side := range []struct {
			name string
			d    spec.Diff
		}{{"theirs (projects/)", c.Theirs}, {"ours (the ticket's copy)", c.Ours}} {
			fmt.Fprintf(out, "  %s:\n", side.name)
			lines := side.d.Summary()
			if len(lines) == 0 {
				lines = []string{"no requirement change"}
			}
			for _, l := range lines {
				fmt.Fprintf(out, "    %s\n", l)
			}
		}
		for _, cl := range c.Clashes {
			var what bytes.Buffer
			if len(cl.Requirements) > 0 {
				what.WriteString("both changed " + strings.Join(cl.Requirements, ", "))
			} else {
				what.WriteString("both changed the file, no requirement in common")
			}
			if cl.Interfaces {
				what.WriteString("; both changed its interfaces")
			}
			fmt.Fprintf(out, "  clash: %s (%s): %s\n", cl.Capability, cl.File, what.String())
		}
		fmt.Fprintf(out, "  resolve: after merging into %s, re-run with --resolve %s=merged,\n", c.Copy, pr.Slug)
		fmt.Fprintf(out, "    or --resolve %s=theirs (drop the copy's conflicting files) or --resolve %s=ours (replace them with the copy)\n", pr.Slug, pr.Slug)
	}
}

// needsStart fails when the ticket's copy for slug holds files but has no
// snapshot to tell another ticket's change from its own: the copy was made
// without ct spec start. It also fails while a ct spec start's rebase is
// unfinished (pendingRebase).
func needsStart(ctx vault.Context, id, slug string) error {
	cp, err := spec.ReadTree(spec.CopyDir(ctx.Vault, id, slug))
	if err != nil || len(cp) == 0 {
		return err
	}
	base, err := spec.ReadSnapshot(spec.BaseDir(ctx.Vault, id, slug))
	if err != nil {
		return err
	}
	if base != nil {
		return pendingRebase(ctx, id, slug)
	}
	return fmt.Errorf("%s: the copy %s/ has no snapshot %s/: run ct spec start %s %s first (it snapshots projects/%s/specs/ as it is now; another ticket's change made before that can't be told from this ticket's)",
		slug, vaultRel(ctx.Vault, spec.CopyDir(ctx.Vault, id, slug)), vaultRel(ctx.Vault, spec.BaseDir(ctx.Vault, id, slug)), id, slug, slug)
}

// pendingRebase refuses a repo whose ct spec start stopped between its two
// snapshot writes (spec.PendingRebase): until a start finishes it, a
// rebased file's base and after disagree.
func pendingRebase(ctx vault.Context, id, slug string) error {
	if err := spec.PendingRebase(ctx.Vault, id, slug); err != nil {
		if errors.Is(err, spec.ErrRebasePending) {
			return fmt.Errorf("%w, then promote", err)
		}
		return err
	}
	return nil
}

// refuseUnfilled refuses a promotion whose after snapshot lacks a file the
// ticket promoted earlier (a move under an older ct that stopped part way)
// when another ticket promoted that file since: projects/ then holds that
// ticket's content, and taking it (spec.TicketAfter) would record it as
// this ticket's change. Without such a promotion projects/ still holds the
// file as this ticket moved it.
func refuseUnfilled(ctx vault.Context, id, slug string, after, projects spec.Tree, v spec.Version, cp spec.Tree) error {
	files := spec.Unfilled(after, projects, v.Files, cp)
	if len(files) == 0 {
		return nil
	}
	versions, err := spec.ListVersions(ctx.Vault, slug)
	if err != nil {
		return err
	}
	for _, f := range files {
		o, ok := spec.PromotedSince(versions, id, f, v.Promoted)
		if !ok {
			continue
		}
		return fmt.Errorf("%s: %s/ lacks %s, which this ticket promoted (a promotion stopped part way), and %s promoted it since (%s, promoted %s): projects/%s/specs/ no longer holds this ticket's content. Run ct spec start %s %s %s to copy it again, redo the change, then promote",
			slug, vaultRel(ctx.Vault, spec.AfterDir(ctx.Vault, id, slug)), f, o.Version.ID(), vaultRel(ctx.Vault, o.Path), o.Version.Promoted, slug, id, slug, strings.SplitN(f, "/", 2)[0])
	}
	return nil
}

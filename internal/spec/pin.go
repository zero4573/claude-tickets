package spec

import (
	"fmt"
	"strings"
	"time"

	"github.com/zero4573/tix/internal/gitx"
)

// PushedHead is the commit of branch in the checkout dir when the push
// landed: refs/heads/<branch> and refs/remotes/origin/<branch> are the same
// commit. ok is false (with no error) when they differ or origin has no such
// branch; a missing local branch is an error.
func PushedHead(dir, branch string) (sha string, ok bool, err error) {
	local, err := gitx.OutIn(dir, nil, "rev-parse", "-q", "--verify", "refs/heads/"+branch+"^{commit}")
	if err != nil || local == "" {
		return "", false, fmt.Errorf("no branch %s in %s: %v", branch, dir, err)
	}
	remote, err := gitx.Out(dir, "rev-parse", "-q", "--verify", "refs/remotes/origin/"+branch+"^{commit}")
	if err != nil || remote != local {
		return "", false, nil
	}
	return local, true, nil
}

// Pin pins v to the pushed commit sha at now. It reports false, changing
// nothing, when v is already pinned to sha; a new commit (a first push, or
// the branch rewritten and pushed again) clears what was resolved for the
// old one (merged, tags).
func Pin(v *Version, sha string, now time.Time) bool {
	if v.Commit == sha {
		return false
	}
	v.ClearPin()
	v.Commit, v.Pinned = sha, now.Format(TimeLayout)
	return true
}

// IsAncestor reports whether commit a is an ancestor of (or is) b in dir.
func IsAncestor(dir, a, b string) bool {
	return gitx.Ok(dir, "merge-base", "--is-ancestor", a, b)
}

func hasCommit(dir, sha string) bool {
	return gitx.Ok(dir, "cat-file", "-e", sha+"^{commit}")
}

// patchIDs reads git patch-id --stable output: "<patch-id> <commit>" lines.
func patchIDs(dir, patch string) ([][2]string, error) {
	if strings.TrimSpace(patch) == "" {
		return nil, nil
	}
	out, err := gitx.OutIn(dir, strings.NewReader(patch), "patch-id", "--stable")
	if err != nil {
		return nil, err
	}
	var ids [][2]string
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) == 2 {
			ids = append(ids, [2]string{f[0], f[1]})
		}
	}
	return ids, nil
}

// BranchPatchID is the stable patch-id of the whole change of commit since
// it left baseRef (git diff <merge-base>..<commit>): what a squash merge of
// the branch applies. Empty when the change is empty.
func BranchPatchID(dir, baseRef, commit string) (string, error) {
	mb, err := gitx.Out(dir, "merge-base", baseRef, commit)
	if err != nil || mb == "" {
		return "", fmt.Errorf("no merge base of %s and %s", baseRef, short(commit))
	}
	patch, err := gitx.Out(dir, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", mb, commit)
	if err != nil {
		return "", err
	}
	ids, err := patchIDs(dir, "commit "+commit+"\n"+patch+"\n")
	if err != nil || len(ids) == 0 {
		return "", err
	}
	return ids[0][0], nil
}

// ResolveMerged finds the commit of baseRef that holds v's pinned commit:
// the commit itself when baseRef contains it (a merge commit, a
// fast-forward); else the first commit on baseRef since the pin, oldest
// first, whose patch-id equals the branch's whole change (a squash or a
// one-commit rebase). A branch updated from the base or a squash with
// conflict fixes doesn't match: "" with no error, as for an unpinned
// version or a commit dir doesn't have.
func ResolveMerged(dir, baseRef string, v Version) (string, error) {
	if v.Commit == "" || !hasCommit(dir, v.Commit) {
		return "", nil
	}
	if !gitx.HasRef(dir, baseRef+"^{commit}") {
		return "", fmt.Errorf("base %s does not resolve in %s", baseRef, dir)
	}
	if IsAncestor(dir, v.Commit, baseRef) {
		return v.Commit, nil
	}
	want, err := BranchPatchID(dir, baseRef, v.Commit)
	if err != nil || want == "" {
		return "", err
	}
	args := []string{"log", "-p", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", "--reverse", "--format=commit %H"}
	if since := sinceArg(v.Pinned); since != "" {
		args = append(args, "--since="+since)
	}
	patch, err := gitx.Out(dir, append(args, baseRef, "--")...)
	if err != nil {
		return "", err
	}
	ids, err := patchIDs(dir, patch)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		if id[0] == want {
			return id[1], nil
		}
	}
	return "", nil
}

// TagsContaining is the tags whose commit holds sha, oldest version first.
func TagsContaining(dir, sha string) ([]string, error) {
	if sha == "" || !hasCommit(dir, sha) {
		return nil, nil
	}
	out, err := gitx.Out(dir, "tag", "--contains", sha, "--sort=version:refname")
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, t := range strings.Split(out, "\n") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	return tags, nil
}

// Refresh resolves a pinned v against baseRef in dir: merged when it is
// still empty (ResolveMerged), then the tags that hold merged (or the
// commit, unmerged). It reports whether v changed; an unpinned v is left
// as it is.
func Refresh(dir, baseRef string, v *Version) (bool, error) {
	if v.Commit == "" {
		return false, nil
	}
	changed := false
	if v.Merged == "" {
		m, err := ResolveMerged(dir, baseRef, *v)
		if err != nil {
			return false, err
		}
		if m != "" {
			v.Merged, changed = m, true
		}
	}
	at := v.Merged
	if at == "" {
		at = v.Commit
	}
	tags, err := TagsContaining(dir, at)
	if err != nil {
		return changed, err
	}
	if strings.Join(tags, "\n") != strings.Join(v.Tags, "\n") {
		v.Tags, changed = tags, true
	}
	return changed, nil
}

// VersionSides gives the two sides of a version's change from its ticket's
// snapshots: before is specs-base/<slug>/, after is specs-after/<slug>/
// (the after rule, Sides, with the version's files). A version without an
// after snapshot (promoted before there was one) has as after side the base
// with its promoted files from projects/ laid on. Without snapshots both
// sides are empty. While the ticket's snapshots are part way rebased
// (PendingRebase) or a promote of them is pending (PendingPromote) it
// refuses rather than read them.
func VersionSides(vault, slug string, v Version) (before, after Tree, err error) {
	id := v.ID()
	if err := Pending(vault, id, slug); err != nil {
		return nil, nil, err
	}
	base, err := ReadSnapshot(BaseDir(vault, id, slug))
	if err != nil {
		return nil, nil, err
	}
	snap, err := ReadSnapshot(AfterDir(vault, id, slug))
	if err != nil {
		return nil, nil, err
	}
	switch {
	case snap != nil:
		before, after = Sides(snap, base, snap, Tree{}, v.Files)
		return before, after, nil
	case base != nil:
		projects, err := ReadTree(VaultDir(vault, slug))
		if err != nil {
			return nil, nil, err
		}
		return base, Overlay(base, Pick(projects, v.Files)), nil
	}
	return Tree{}, Tree{}, nil
}

// UnderCapability keeps the files of t under capability's folder.
func UnderCapability(t Tree, capability string) Tree {
	out := Tree{}
	for k, b := range t {
		if strings.HasPrefix(k, capability+"/") {
			out[k] = b
		}
	}
	return out
}

// sinceArg turns a version's pinned time (local, without a zone) into a
// --since bound two days earlier, so a squash committed on a machine whose
// clock or zone differs from the pinning one is still searched; "" when the
// time doesn't parse, which searches the whole base.
func sinceArg(pinned string) string {
	t, err := time.ParseInLocation(TimeLayout, pinned, time.Local)
	if err != nil {
		return ""
	}
	return t.Add(-48 * time.Hour).Format(time.RFC3339)
}

// Reapply reads the version note at path again and applies a pin to sha
// (none when sha is "") and Refresh to what is there. Called under the vault
// lock, it keeps a save that rewrote the note since it was first read (new
// files, pin cleared). It returns the fresh version, whether it was pinned
// here, and whether anything changed.
func Reapply(path, dir, baseRef, sha string, now time.Time) (v Version, pinned, changed bool, err error) {
	if v, err = ReadVersion(path); err != nil {
		return v, false, false, err
	}
	if sha != "" {
		pinned = Pin(&v, sha, now)
	}
	changed, err = Refresh(dir, baseRef, &v)
	return v, pinned, changed || pinned, err
}

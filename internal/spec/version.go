package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zero4573/tix/internal/fsx"
	"github.com/zero4573/tix/internal/note"
)

// VersionType is the type: of a vault spec version note.
const VersionType = "spec-version"

// TimeLayout is the format of a version note's promoted and pinned times.
const TimeLayout = "2006-01-02T15:04:05"

// Version is the frontmatter of a vault spec version note: one per ticket
// and repo, at projects/<slug>/spec-versions/<slug>-spec-<ID>.md. Files
// accumulates every file the ticket promoted; Commit, Pinned, Merged and
// Tags are the pin (empty: unpinned).
type Version struct {
	Type     string   `yaml:"type" json:"-"`
	Slug     string   `yaml:"slug" json:"slug"`
	Ticket   string   `yaml:"ticket" json:"ticket"`
	Project  string   `yaml:"project" json:"project"`
	Promoted string   `yaml:"promoted" json:"promoted"`
	Files    []string `yaml:"files,flow" json:"files"`
	Branch   string   `yaml:"branch" json:"branch"`
	Commit   string   `yaml:"commit" json:"commit"`
	Pinned   string   `yaml:"pinned" json:"pinned"`
	Merged   string   `yaml:"merged" json:"merged"`
	Tags     []string `yaml:"tags,flow" json:"tags"`
}

// ID is the ticket ID of the version ("[[PROJ-12]]" gives PROJ-12).
func (v Version) ID() string {
	return strings.TrimSuffix(strings.TrimPrefix(v.Ticket, "[["), "]]")
}

// ClearPin empties the pin: it described content the ticket has since
// replaced.
func (v *Version) ClearPin() {
	v.Commit, v.Pinned, v.Merged, v.Tags = "", "", "", nil
}

// HasFile reports whether the version promoted file.
func (v Version) HasFile(file string) bool {
	for _, f := range v.Files {
		if f == file {
			return true
		}
	}
	return false
}

// VersionsDir is where a slug's version notes live.
func VersionsDir(vault, slug string) string {
	return filepath.Join(vault, "projects", slug, "spec-versions")
}

// VersionPath is the version note of ticket id for slug.
func VersionPath(vault, slug, id string) string {
	return filepath.Join(VersionsDir(vault, slug), slug+"-spec-"+id+".md")
}

// CopyDir is the ticket's working copy of a vault spec.
func CopyDir(vault, id, slug string) string {
	return filepath.Join(note.TicketDir(vault, id), "specs", slug)
}

// BaseDir is the before snapshot: projects/<slug>/specs/ when tix spec start
// first made the ticket's copy.
func BaseDir(vault, id, slug string) string {
	return filepath.Join(note.TicketDir(vault, id), "specs-base", slug)
}

// AfterDir is the after snapshot: projects/<slug>/specs/ right after the
// ticket's last promotion.
func AfterDir(vault, id, slug string) string {
	return filepath.Join(note.TicketDir(vault, id), "specs-after", slug)
}

// RebaseMarker is the file tix spec start writes before it rebases the
// snapshots and removes once both are written: it lists the files being
// rebased, so a run that stopped between the base and the after snapshot
// is finished by the next tix spec start, and tix spec promote refuses until
// then. It sits beside the base snapshot, outside it.
func RebaseMarker(vault, id, slug string) string {
	return filepath.Join(note.TicketDir(vault, id), "specs-base", "."+slug+".rebase")
}

// KeptList is the list of files tix spec promote --resolve <slug>=theirs
// kept as projects/ holds them: the after snapshot holds them (so a later
// change made on top of theirs isn't refused), and every later promotion
// carries them over. They are not promoted files: the version note, its
// diff and its sides leave them out. It sits beside the after snapshot,
// outside it.
func KeptList(vault, id, slug string) string {
	return filepath.Join(note.TicketDir(vault, id), "specs-after", "."+slug+".kept")
}

// ReadKept reads a KeptList file, sorted; none when it doesn't exist.
func ReadKept(path string) ([]string, error) {
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
	return MergeFiles(out, nil), nil
}

// WriteKept writes a KeptList file atomically, one file per line, sorted.
func WriteKept(path string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsx.WriteFileAtomic(path, []byte(strings.Join(MergeFiles(files, nil), "\n")+"\n"), 0o644)
}

// PendingRebase is nil unless ticket id's rebase marker for slug exists
// (RebaseMarker): its snapshots are then part way rebased, a rebased file's
// base and after disagree, and nothing should read them until the next
// tix spec start finishes them. The error names that rerun.
func PendingRebase(vault, id, slug string) error {
	_, err := os.Lstat(RebaseMarker(vault, id, slug))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return &pendingRebaseError{fmt.Sprintf("%s: the snapshots of %s are part way rebased (a tix spec start stopped while it rebased them): run tix spec start %s %s again to finish them", slug, id, id, slug)}
}

// ErrRebasePending matches (errors.Is) what PendingRebase returns, so a
// caller can tell a marked ticket from a failed read.
var ErrRebasePending = errors.New("snapshots part way rebased")

type pendingRebaseError struct{ msg string }

func (e *pendingRebaseError) Error() string        { return e.msg }
func (e *pendingRebaseError) Is(target error) bool { return target == ErrRebasePending }

// RefreshPending is the note a listing gives a version whose ticket's
// rebase marker is there: only that ticket's snapshots are unreadable.
func RefreshPending(id, slug string) string {
	return fmt.Sprintf("refresh pending: run tix spec start %s %s", id, slug)
}

// ReadSnapshot reads a snapshot folder; nil (not an empty Tree) when the
// folder doesn't exist.
func ReadSnapshot(dir string) (Tree, error) {
	if st, err := os.Lstat(dir); err != nil || !st.IsDir() {
		return nil, nil
	}
	return ReadTree(dir)
}

// ReadVersion reads a version note's frontmatter. A missing note gives an
// error that matches os.ErrNotExist.
func ReadVersion(path string) (Version, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Version{}, err
	}
	var v Version
	fm, ok := frontmatterBlock(b)
	if !ok {
		return Version{}, fmt.Errorf("%s: no frontmatter", path)
	}
	if err := yaml.Unmarshal(fm, &v); err != nil {
		return Version{}, fmt.Errorf("%s: %w", path, err)
	}
	if v.Type != VersionType {
		return Version{}, fmt.Errorf("%s: type is %q, not %s", path, v.Type, VersionType)
	}
	return v, nil
}

func frontmatterBlock(b []byte) ([]byte, bool) {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(b, []byte("---\n")) {
		return nil, false
	}
	rest := b[4:]
	if i := bytes.Index(rest, []byte("\n---\n")); i >= 0 {
		return rest[:i+1], true
	}
	if bytes.HasSuffix(rest, []byte("\n---")) {
		return rest[:len(rest)-3], true
	}
	return nil, false
}

// VersionFile is a version note and its path.
type VersionFile struct {
	Path    string  `json:"path"`
	Version Version `json:"version"`
}

// ListVersions reads the version notes of slug, newest promoted first
// (then by ticket ID). Notes that don't parse are skipped; no folder gives
// none.
func ListVersions(vault, slug string) ([]VersionFile, error) {
	entries, err := os.ReadDir(VersionsDir(vault, slug))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []VersionFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(VersionsDir(vault, slug), e.Name())
		v, err := ReadVersion(p)
		if err != nil || v.Slug != slug {
			continue
		}
		out = append(out, VersionFile{Path: p, Version: v})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Version, out[j].Version
		if a.Promoted != b.Promoted {
			return a.Promoted > b.Promoted
		}
		return a.ID() < b.ID()
	})
	return out, nil
}

// Keys is the paths of t, sorted.
func (t Tree) Keys() []string { return t.paths() }

// Pick keeps only the files of t named in files.
func Pick(t Tree, files []string) Tree {
	out := Tree{}
	for _, f := range files {
		if b, ok := t[f]; ok {
			out[f] = b
		}
	}
	return out
}

// MergeFiles is the sorted union of two file lists.
func MergeFiles(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range append(append([]string{}, a...), b...) {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// PinStale reports whether a promotion that turns the after snapshot old
// into cur changed any of files: the pin then described other content.
func PinStale(old, cur Tree, files []string) bool {
	for _, f := range files {
		a, aok := old[f]
		b, bok := cur[f]
		if aok != bok || !bytes.Equal(a, b) {
			return true
		}
	}
	return false
}

// TicketAfter is the after snapshot a promotion writes: this ticket's files
// only, never the rest of projects/. It holds prev's entries of the files
// promoted (or kept as theirs) earlier (projects' when there is no prev: a
// ticket promoted before there were after snapshots) with now, the files
// promoted now, laid on. A file another ticket changed in projects/ since
// keeps this ticket's content, so neither the diff nor the version history
// takes that change for this ticket's. A promoted file prev and now both
// lack takes projects' entry (Unfilled lists them): promote writes the
// after snapshot with the copy's files before it moves them, so this only
// happens for a move that stopped part way under an older tix; the caller
// refuses when another ticket promoted such a file since this ticket did,
// as projects/ then holds that ticket's content, not this one's.
func TicketAfter(prev, projects Tree, promoted []string, now Tree) Tree {
	if prev == nil {
		prev = projects
	}
	return Overlay(Overlay(Pick(projects, Unfilled(prev, projects, promoted, now)), Pick(prev, promoted)), now)
}

// Unfilled lists the promoted files TicketAfter takes from projects: prev
// (not nil) and now lack them, projects holds them.
func Unfilled(prev, projects Tree, promoted []string, now Tree) []string {
	if prev == nil {
		return nil
	}
	var out []string
	for _, f := range promoted {
		_, inPrev := prev[f]
		_, inNow := now[f]
		_, inProj := projects[f]
		if !inPrev && !inNow && inProj {
			out = append(out, f)
		}
	}
	return out
}

// PromotedSince is the newest version note in versions, other than ticket
// id's, that lists file and was promoted after since (a version note's
// promoted time); false when there is none.
func PromotedSince(versions []VersionFile, id, file, since string) (VersionFile, bool) {
	for _, vf := range versions {
		if vf.Version.ID() != id && vf.Version.Promoted > since && vf.Version.HasFile(file) {
			return vf, true
		}
	}
	return VersionFile{}, false
}

// ChangedBy is the version notes in versions (newest promoted first, as
// ListVersions gives them), other than ticket id's, that list file and
// whose ticket recorded in its after snapshot (specs-after/<slug>/) the
// content projects/ holds now: cur, or no file when ok is false. They are
// the tickets whose promotion left the file as it is; none means someone
// else changed it (a hand edit). Promoted times don't decide it: a
// ticket's note is stamped when it planned, not when its put ran.
func ChangedBy(vault, slug string, versions []VersionFile, id, file string, cur []byte, ok bool) ([]VersionFile, error) {
	var out []VersionFile
	for _, vf := range versions {
		o := vf.Version.ID()
		if o == id || !vf.Version.HasFile(file) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(AfterDir(vault, o, slug), filepath.FromSlash(file)))
		if errors.Is(err, os.ErrNotExist) {
			if !ok {
				// a deletion only when the ticket recorded an after
				// snapshot of this repo at all. A partial move under an
				// older tix (an after snapshot lacking a file the note
				// lists) also reads as a deletion here: accepted, it only
				// names that ticket in a report or refusal
				if _, err := os.Stat(AfterDir(vault, o, slug)); err == nil {
					out = append(out, vf)
				} else if !errors.Is(err, os.ErrNotExist) {
					return nil, err
				}
			}
			continue
		} else if err != nil {
			return nil, err
		}
		if ok && bytes.Equal(b, cur) {
			out = append(out, vf)
		}
	}
	return out, nil
}

// Sides gives the two sides of a ticket's vault spec change (the after
// rule). projects is projects/<slug>/specs/, base and after the ticket's
// snapshots (nil: missing), copy its working copy and promoted the files
// its version note lists:
//   - no base: before = projects, after = projects with the copy laid on;
//   - base and after: before = base, after = base with the promoted files
//     of the after snapshot and then the copy laid on, so another
//     ticket's later promotion never enters this ticket's change;
//   - base only, nothing promoted yet (tix spec start took the base):
//     before = projects with the base's version of the copy's files laid
//     on, after = projects with the copy laid on, so another ticket's
//     promotion since the start shows on neither side;
//   - base only with promoted files (a ticket promoted before snapshots
//     had an after side): before = base, after = projects with the copy
//     laid on.
func Sides(projects, base, after, copy Tree, promoted []string) (before, aft Tree) {
	switch {
	case base == nil:
		return projects, Overlay(projects, copy)
	case after != nil:
		return base, Overlay(base, Overlay(Pick(after, promoted), copy))
	case len(promoted) == 0:
		return Overlay(projects, Pick(base, copy.paths())), Overlay(projects, copy)
	}
	return base, Overlay(projects, copy)
}

// Conflicts lists the files of the copy that another ticket changed in
// projects/ since this ticket's snapshots: the projects/ file differs from
// the base and, when the after snapshot holds it, from that entry too (the
// after holds only this ticket's files, so a file it lacks is compared with
// the base alone). Without a base there is nothing to compare with: none.
func Conflicts(projects, base, after, copy Tree) []string {
	if base == nil {
		return nil
	}
	var out []string
	for _, f := range copy.paths() {
		if _, ok := after[f]; SameFile(base, projects, f) || (ok && SameFile(after, projects, f)) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// SameFile reports whether a and b hold file f alike: both without it, or
// both with the same bytes.
func SameFile(a, b Tree, f string) bool {
	x, xok := a[f]
	y, yok := b[f]
	return xok == yok && bytes.Equal(x, y)
}

// Rebase moves the entries of files in the base snapshot, and in the after
// snapshot when there is one (nil: none), onto projects: the content a copy
// file that tix spec start writes from projects/ starts at, so the diff and
// the conflict check measure the ticket's change from there. The after
// snapshot holds the ticket's promoted files only, so only an entry it has
// is moved; a file it lacks stays out of it. It returns the
// new snapshots (base and after themselves are left as they are) and the
// files whose entry changed in either, sorted; a file projects lacks is
// left out.
func Rebase(projects, base, after Tree, files []string) (newBase, newAfter Tree, changed []string) {
	newBase, newAfter = Overlay(base, nil), after
	if after != nil {
		newAfter = Overlay(after, nil)
	}
	seen := map[string]bool{}
	for _, f := range files {
		b, ok := projects[f]
		if !ok || seen[f] {
			continue
		}
		seen[f] = true
		moved := !SameFile(base, projects, f)
		newBase[f] = b
		if _, ok := after[f]; ok {
			moved = moved || !SameFile(after, projects, f)
			newAfter[f] = b
		}
		if moved {
			changed = append(changed, f)
		}
	}
	sort.Strings(changed)
	return newBase, newAfter, changed
}

// Ancestor is the common ancestor of a conflict's two sides for files, per
// file: the after snapshot's entry when it has one (the file as this
// ticket last promoted it), else the base's. The after snapshot holds only
// the files the ticket promoted.
func Ancestor(base, after Tree, files []string) Tree {
	out := Tree{}
	for _, f := range files {
		if b, ok := after[f]; ok {
			out[f] = b
		} else if b, ok := base[f]; ok {
			out[f] = b
		}
	}
	return out
}

// Clash is one capability both sides of a conflict changed: Requirements
// are the IDs both changed (none: they changed different requirements of
// the same file); Interfaces is set when both changed its interfaces.
type Clash struct {
	Capability   string   `json:"capability"`
	File         string   `json:"file"`
	Requirements []string `json:"requirements"`
	Interfaces   bool     `json:"interfaces"`
}

// Clashes compares two changes of the same files (theirs and ours, from
// one ancestor) by capability: one Clash per file both diffs touch.
func Clashes(theirs, ours Diff) []Clash {
	caps := map[string]CapChange{}
	for _, c := range theirs.Caps {
		caps[c.File] = c
	}
	theirFiles := map[string]bool{}
	for _, f := range theirs.Files {
		theirFiles[f.Path] = true
	}
	var out []Clash
	for _, f := range ours.Files {
		if !theirFiles[f.Path] {
			continue
		}
		cl := Clash{File: f.Path, Capability: strings.SplitN(f.Path, "/", 2)[0], Requirements: []string{}}
		var oc CapChange
		for _, c := range ours.Caps {
			if c.File == f.Path {
				oc = c
				if c.Capability != "" {
					cl.Capability = c.Capability
				}
			}
		}
		tc := caps[f.Path]
		ids := map[string]bool{}
		for _, ch := range tc.Changes {
			ids[ch.Req.ID] = true
		}
		for _, ch := range oc.Changes {
			if ids[ch.Req.ID] {
				cl.Requirements = append(cl.Requirements, ch.Req.ID)
			}
		}
		cl.Interfaces = tc.Interfaces != "" && oc.Interfaces != ""
		out = append(out, cl)
	}
	return out
}

// VersionState is the first line of a version note's body.
func VersionState(v Version) string {
	if v.Commit == "" {
		return "Unpinned: promoted, not pushed yet"
	}
	s := "Pinned to " + short(v.Commit)
	if v.Branch != "" {
		s += " on " + v.Branch
	}
	if v.Merged != "" {
		s += ", merged as " + short(v.Merged)
	}
	if len(v.Tags) > 0 {
		s += ", in " + v.Tags[0]
	}
	return s
}

// yamlScalar writes s plain when YAML reads it back as the same string
// (and it is safe in a flow list), else double-quoted.
func yamlScalar(s string) string {
	if s != "" && !strings.ContainsAny(s, ",[]{}#&*!|>'\"%@`") && !strings.Contains(s, ": ") && !strings.HasSuffix(s, ":") && strings.TrimSpace(s) == s {
		var back string
		if out, err := yaml.Marshal(s); err == nil && strings.TrimSpace(string(out)) == s && yaml.Unmarshal(out, &back) == nil && back == s {
			return s
		}
	}
	q, _ := json.Marshal(s)
	return string(q)
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// RenderVersion writes a whole version note: the frontmatter, then a
// generated body (the state, links to each changed capability's before and
// after file in the ticket's snapshots (before only when the file is in
// before), the change as RenderChange prints it). ticketRel is the ticket folder relative to the vault
// (tickets/<source>/<ID>).
func RenderVersion(v Version, ticketRel string, before Tree, d Diff) ([]byte, error) {
	v.Type = VersionType
	if v.Files == nil {
		v.Files = []string{}
	}
	if v.Tags == nil {
		v.Tags = []string{}
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	for _, kv := range []struct {
		k string
		v any
	}{
		{"type", v.Type}, {"slug", v.Slug}, {"ticket", v.Ticket}, {"project", v.Project},
		{"promoted", v.Promoted}, {"files", v.Files}, {"branch", v.Branch}, {"commit", v.Commit},
		{"pinned", v.Pinned}, {"merged", v.Merged}, {"tags", v.Tags},
	} {
		switch x := kv.v.(type) {
		case string:
			fmt.Fprintf(&b, "%s: %s\n", kv.k, yamlScalar(x))
		case []string:
			items := make([]string, len(x))
			for i, it := range x {
				items[i] = yamlScalar(it)
			}
			fmt.Fprintf(&b, "%s: [%s]\n", kv.k, strings.Join(items, ", "))
		}
	}
	b.WriteString("---\n")
	fmt.Fprintf(&b, "# %s spec version %s\n\n", v.Slug, v.ID())
	b.WriteString("Generated by tix spec promote; edits are overwritten.\n\n")
	b.WriteString(VersionState(v) + "\n")
	if len(d.Caps) > 0 {
		b.WriteString("\n")
	}
	for _, c := range d.Caps {
		link := func(snap, label string) string {
			return "[[" + ticketRel + "/" + snap + "/" + v.Slug + "/" + strings.TrimSuffix(c.File, ".md") + "|" + label + "]]"
		}
		var parts []string
		if _, ok := before[c.File]; ok {
			parts = append(parts, link("specs-base", "before"))
		}
		parts = append(parts, link("specs-after", "after"))
		fmt.Fprintf(&b, "- %s: %s\n", c.Capability, strings.Join(parts, " · "))
	}
	if ch := RenderChange(d); ch != "" {
		b.WriteString("\n## Change\n\n" + ch)
		if !strings.HasSuffix(ch, "\n") {
			b.WriteString("\n")
		}
	}
	return b.Bytes(), nil
}

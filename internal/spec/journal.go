package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/zero4573/tix/internal/config"
	"github.com/zero4573/tix/internal/note"
)

// JournalHeader is a promote journal's first line.
const JournalHeader = "promote-journal v2"

// The operations of a promote journal (JournalPath), one per line after
// JournalHeader, applied in order by tix spec promote. Every put comes
// before any drop.
const (
	// OpReplaceTree "replace-tree base|after": the staged tree replaces
	// specs-base/<slug>/ or specs-after/<slug>/.
	OpReplaceTree = "replace-tree"
	// OpWrite "write kept|note <vault-relative dest>": the staged kept list
	// or version note is written to dest.
	OpWrite = "write"
	// OpPut "put <file> <pre>": the staged moves/<file> is written into
	// projects/<slug>/specs/<file>; pre is that file's hash at plan time,
	// or PreAbsent.
	OpPut = "put"
	// OpTrash "trash <file> sha256:<hex>": the copy's file, when it still
	// has that hash, goes to the vault's .trash/.
	OpTrash = "trash"
	// OpDrop "drop <file> sha256:<hex>": the copy's file, when it still has
	// that hash, is removed (its links were pointed at projects/ first).
	OpDrop = "drop"
)

// What an OpReplaceTree or OpWrite names.
const (
	TreeBase    = "base"
	TreeAfter   = "after"
	WriteOfKept = "kept"
	WriteOfNote = "note"
)

// PreAbsent is a put's pre-image when projects/ had no such file at plan
// time.
const PreAbsent = "absent"

// Op is one line of a promote journal: Kind, its argument (a tree, a
// write's kind, or a "/"-separated file of the copy or projects/), a
// write's vault-relative destination, and the hash of a put (its
// pre-image), trash or drop (the copy file's plan-time content).
type Op struct {
	Kind, Arg, Dest, Hash string
}

func (o Op) String() string {
	switch o.Kind {
	case OpWrite:
		return o.Kind + " " + o.Arg + " " + o.Dest
	case OpPut, OpTrash, OpDrop:
		return o.Kind + " " + o.Arg + " " + o.Hash
	}
	return o.Kind + " " + o.Arg
}

// Hash is the journal's form of b's content hash: sha256:<lowercase hex>.
func Hash(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func validHash(h string) bool {
	x, ok := strings.CutPrefix(h, "sha256:")
	if !ok || len(x) != 64 {
		return false
	}
	for _, c := range x {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// StageRoot is the ticket's promote staging folder: specs-stage/ beside
// specs-base/ and specs-after/.
func StageRoot(vault, id string) string {
	return filepath.Join(note.TicketDir(vault, id), "specs-stage")
}

// StageDir holds the full new content of every file a promote of slug
// writes: base/ and after/ (trees), kept, note.md and moves/ (projects/
// files), and prev/: the plan-time base and after entries of each put file
// and the plan-time note (prev/base/, prev/after/, prev/note.md), with
// prev/absent listing those that didn't exist (PrevAbsent).
func StageDir(vault, id, slug string) string {
	return filepath.Join(StageRoot(vault, id), slug)
}

// JournalPath is the promote journal of ticket id for slug, beside its
// stage. It is written last, once the stage is complete: its presence means
// the promote passed its commit point and a rerun finishes it.
func JournalPath(vault, id, slug string) string {
	return filepath.Join(StageRoot(vault, id), slug+".journal")
}

// DonePath is the journal's progress log: the line number of each
// operation done, one "<n>\n" appended per operation.
func DonePath(vault, id, slug string) string {
	return JournalPath(vault, id, slug) + ".done"
}

// HeldPath lists the files a replay held back (one per line): they changed
// in projects/ (by another ticket or by hand) after this promote planned.
func HeldPath(vault, id, slug string) string {
	return filepath.Join(StageRoot(vault, id), slug+".held")
}

// RenderJournal is the text of a journal of ops, its header first.
func RenderJournal(ops []Op) []byte {
	var b strings.Builder
	b.WriteString(JournalHeader + "\n")
	for _, o := range ops {
		b.WriteString(o.String())
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// cleanRel reports whether f is a clean, relative, "/"-separated path that
// stays inside its folder.
func cleanRel(f string) bool {
	return f != "" && f != "." && !strings.HasPrefix(f, "/") && !strings.Contains(f, `\`) &&
		path.Clean(f) == f && f != ".." && !strings.HasPrefix(f, "../") && filepath.VolumeName(filepath.FromSlash(f)) == ""
}

// PlainName reports whether f holds no control character (a newline or
// carriage return included): only such a name fits on a journal line.
func PlainName(f string) bool {
	return !strings.ContainsFunc(f, unicode.IsControl)
}

// JournalPathOK reports whether f is a path a journal line may name.
func JournalPathOK(f string) bool {
	return cleanRel(f) && PlainName(f)
}

// ParseJournal reads a journal's text; a missing header or any line it
// doesn't know is an error, so a damaged journal is never half applied.
func ParseJournal(b []byte) ([]Op, error) {
	s := string(b)
	head, rest, _ := strings.Cut(s, "\n")
	if head != JournalHeader {
		return nil, fmt.Errorf("first line %q, want %q", head, JournalHeader)
	}
	var ops []Op
	drops := false
	for i, l := range strings.Split(rest, "\n") {
		if l == "" {
			continue
		}
		kind, rest, _ := strings.Cut(l, " ")
		o := Op{Kind: kind, Arg: rest}
		ok := false
		switch kind {
		case OpReplaceTree:
			ok = rest == TreeBase || rest == TreeAfter
		case OpWrite:
			o.Arg, o.Dest, _ = strings.Cut(rest, " ")
			ok = (o.Arg == WriteOfKept || o.Arg == WriteOfNote) && JournalPathOK(o.Dest)
		case OpPut, OpTrash, OpDrop:
			if j := strings.LastIndex(rest, " "); j >= 0 {
				o.Arg, o.Hash = rest[:j], rest[j+1:]
				ok = JournalPathOK(o.Arg) && (validHash(o.Hash) || (kind == OpPut && o.Hash == PreAbsent))
			}
			if kind == OpDrop {
				drops = true
			} else if kind == OpPut && drops {
				return nil, fmt.Errorf("line %d: a put after a drop: %q", i+1, l)
			}
		}
		if !ok {
			return nil, fmt.Errorf("line %d: %q", i+1, l)
		}
		ops = append(ops, o)
	}
	return ops, nil
}

// ParseDone reads a progress log of a journal of n operations: the line
// numbers done, and the length of its complete lines. A last line without
// its newline is a torn append, ignored (it is the start of a number);
// anything else that isn't a number 1..n, decimal, without sign or leading
// zero, is an error.
func ParseDone(b []byte, n int) (done map[int]bool, complete int, err error) {
	done = map[int]bool{}
	s := string(b)
	lines := strings.Split(s, "\n")
	torn := lines[len(lines)-1]
	lines = lines[:len(lines)-1]
	num := func(l string) (int, bool) {
		if l == "" || l[0] == '0' || len(l) > 9 {
			return 0, false
		}
		v := 0
		for _, c := range l {
			if c < '0' || c > '9' {
				return 0, false
			}
			v = v*10 + int(c-'0')
		}
		return v, true
	}
	for i, l := range lines {
		v, ok := num(l)
		if !ok || v > n {
			return nil, 0, fmt.Errorf("progress line %d: %q", i+1, l)
		}
		done[v] = true
	}
	if torn != "" {
		if _, ok := num(torn); !ok {
			return nil, 0, fmt.Errorf("progress last line: %q", torn)
		}
	}
	return done, len(s) - len(torn), nil
}

// ParseHeld reads a .held list (HeldPath).
func ParseHeld(b []byte) ([]string, error) {
	var out []string
	for i, l := range strings.Split(string(b), "\n") {
		if l == "" {
			continue
		}
		if !JournalPathOK(l) {
			return nil, fmt.Errorf("held line %d: %q", i+1, l)
		}
		out = append(out, l)
	}
	return out, nil
}

// PrevAbsent is the stage file (under prev/) listing the plan-time entries
// that didn't exist, one "base|after|note <clean path>" per line (note:
// the version note's vault-relative path).
const PrevAbsent = "absent"

// ParseAbsent reads prev/absent: for each of base, after and note, the
// paths absent at plan time.
func ParseAbsent(b []byte) (map[string]map[string]bool, error) {
	out := map[string]map[string]bool{TreeBase: {}, TreeAfter: {}, WriteOfNote: {}}
	for i, l := range strings.Split(string(b), "\n") {
		if l == "" {
			continue
		}
		k, f, _ := strings.Cut(l, " ")
		m, ok := out[k]
		if !ok || !JournalPathOK(f) {
			return nil, fmt.Errorf("prev/absent line %d: %q", i+1, l)
		}
		m[f] = true
	}
	return out, nil
}

// RenderAbsent is the text of prev/absent.
func RenderAbsent(base, after []string, note string) []byte {
	var b strings.Builder
	for _, f := range base {
		b.WriteString(TreeBase + " " + f + "\n")
	}
	for _, f := range after {
		b.WriteString(TreeAfter + " " + f + "\n")
	}
	if note != "" {
		b.WriteString(WriteOfNote + " " + note + "\n")
	}
	return []byte(b.String())
}

// ErrJournalDamaged matches (errors.Is) what DamagedJournal returns.
var ErrJournalDamaged = errors.New("damaged promote journal")

type damagedError struct {
	msg   string
	cause error
}

func (e *damagedError) Error() string { return e.msg }
func (e *damagedError) Unwrap() []error {
	return []error{ErrJournalDamaged, e.cause}
}

// DamagedJournal is the refusal of ticket id's damaged journal for slug:
// it names the journal, its stage, progress log and held list, and how to discard
// the pending promote.
func DamagedJournal(vault, id, slug string, cause error) error {
	return &damagedError{
		msg: fmt.Sprintf("damaged promote journal %s (%v); nothing was written. Removing it, its stage %s, %s and %s (if there) discards the pending promote (the copy still holds every file not yet dropped)",
			config.TildePath(JournalPath(vault, id, slug)), cause, config.TildePath(StageDir(vault, id, slug))+string(filepath.Separator), config.TildePath(DonePath(vault, id, slug)), config.TildePath(HeldPath(vault, id, slug))),
		cause: cause,
	}
}

// ReadJournal reads ticket id's promote journal for slug; ok is false when
// there is none. A damaged one is a DamagedJournal error.
func ReadJournal(vault, id, slug string) (ops []Op, ok bool, err error) {
	b, err := os.ReadFile(JournalPath(vault, id, slug))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	ops, err = ParseJournal(b)
	if err != nil {
		return nil, false, DamagedJournal(vault, id, slug, err)
	}
	return ops, true, nil
}

// PendingPromote is nil unless ticket id's promote journal for slug exists:
// a promote passed its commit point and stopped, so the snapshots, note and
// projects/ may be part way written until tix spec promote (or tix spec
// start) finishes it. The error names that rerun.
func PendingPromote(vault, id, slug string) error {
	_, err := os.Lstat(JournalPath(vault, id, slug))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return &pendingPromoteError{fmt.Sprintf("%s: %s", slug, PromotePendingNote(id))}
}

// ErrPromotePending matches (errors.Is) what PendingPromote returns.
var ErrPromotePending = errors.New("promote pending")

type pendingPromoteError struct{ msg string }

func (e *pendingPromoteError) Error() string        { return e.msg }
func (e *pendingPromoteError) Is(target error) bool { return target == ErrPromotePending }

// PromotePendingNote is the note a listing gives a version whose ticket's
// promote journal is there.
func PromotePendingNote(id string) string {
	return "promote pending: run tix spec promote " + id
}

// Pending is PendingRebase, then PendingPromote: nil when ticket id's
// snapshots for slug are settled.
func Pending(vault, id, slug string) error {
	if err := PendingRebase(vault, id, slug); err != nil {
		return err
	}
	return PendingPromote(vault, id, slug)
}

// PendingNote is the listing note for a Pending error; "" for any other
// error.
func PendingNote(err error, id, slug string) string {
	switch {
	case errors.Is(err, ErrRebasePending):
		return RefreshPending(id, slug)
	case errors.Is(err, ErrPromotePending):
		return PromotePendingNote(id)
	}
	return ""
}

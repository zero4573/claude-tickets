package cli

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/diffcheck"
	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/vault"
)

func wsDiffCheckCmd() *cobra.Command {
	var dir, maxSize, ticket string
	cmd := &cobra.Command{
		Use:   "diff-check [-C <dir>] [--max-size <size>] [--ticket <ID>] <rev-range>",
		Short: "Flag binary or oversized files added by the commits of a range",
		Long: `Checks every non-merge commit of <rev-range> (anything git rev-list
accepts, typically HEAD..task/<branch>) and flags each file it adds or
modifies that git treats as binary or whose blob is larger than --max-size
(a byte count, or a number with K, M or G; default 1M). Deletions are
ignored. One line per flagged file on stdout:

  <binary|large|binary,large>  <size in bytes>  <short sha>  <path>

With --ticket <ID> it also flags the ticket's own vault-only IDs (the ID,
the R-nn of its relay.md, the B-nn of its backlog.md, the finding IDs of
its reviews/: entry lines only) that a commit introduces: in a commit
message any whole-word occurrence, in a file an ID that occurs more often
in the commit's added lines than in its removed ones (an ID kept on a
reworded line passes):

  vault-id  <the ID>  <short sha>  <path | :commit-message>

Lines are per commit in rev-list order, its size lines first, then its
vault-id lines, then two informational note lines (they never change the
exit code):

  note  test-changed  <name>  <short sha>  <path>
  note  unsigned  -  <short sha>  :commit

test-changed: the commit changes or removes a test that its first parent
had: a top-level Test*, Benchmark*, Fuzz* or Example* func of a *_test.go
file whose source text (comments included) differs or is gone, or (name -)
an existing testdata/**/*.txtar file it modifies or deletes. Name - also
stands for a test file whose type changed, that no longer parses, or that
is above --max-size. Names follow go test (Testdata is no test). Added
tests and added files are never noted, and a failure to compute notes
yields none. unsigned: the commit object has no gpgsig
header, printed only when commit.gpgsign is true in that checkout.

For a range A..B or A...B it also checks the repo's specs/<capability>/spec.md:
a requirement ID that B adds (not defined at the merge base) but A already
defines, or that B defines twice, is flagged, one line per definition, after
the commit lines (a repo without specs/ is skipped):

  spec-id  <REQ-ID>  <short sha of B>  <spec file>

Exits 0 when clean (notes don't count), 3 when something is flagged, 1 on a usage error, a
directory that is not a git checkout, or a bad range.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, err := diffcheck.ParseSize(maxSize)
			if err != nil {
				return fmt.Errorf("diff-check: %w", err)
			}
			if dir == "" {
				dir = "."
			}
			if _, err := exec.LookPath("git"); err != nil {
				return fmt.Errorf("diff-check: git not found")
			}
			var ids []string
			if ticket != "" {
				ctx, err := vault.Require()
				if err != nil {
					return fmt.Errorf("diff-check: %w", err)
				}
				folder := note.TicketDir(ctx.Vault, ticket)
				if st, err := os.Stat(folder); !note.ValidKey(ticket) || err != nil || !st.IsDir() {
					return fmt.Errorf("diff-check: ticket %s not found in the vault", ticket)
				}
				ids = diffcheck.TicketIDs(folder, ticket)
			}
			found, err := diffcheck.Check(dir, args[0], limit)
			if err != nil {
				return fmt.Errorf("diff-check: %w", err)
			}
			var hits []diffcheck.IDHit
			if ticket != "" {
				if hits, err = diffcheck.CheckIDs(dir, args[0], ids); err != nil {
					return fmt.Errorf("diff-check: %w", err)
				}
			}
			specHits, err := diffcheck.CheckSpecIDs(dir, args[0])
			if err != nil {
				return fmt.Errorf("diff-check: %w", err)
			}
			// per commit in rev-list order: its size lines, then its ID lines
			revs, err := diffcheck.Revs(dir, args[0])
			if err != nil {
				return fmt.Errorf("diff-check: %w", err)
			}
			signing := diffNoteBool(dir, "commit.gpgsign")
			var b strings.Builder
			fi, hi := 0, 0
			for _, sha := range revs {
				short := diffcheck.ShortSHA(sha)
				for ; fi < len(found) && found[fi].Commit == short; fi++ {
					f := found[fi]
					fmt.Fprintf(&b, "%s\t%d\t%s\t%s\n", f.Kind, f.Size, f.Commit, f.Path)
				}
				for ; hi < len(hits) && hits[hi].Commit == short; hi++ {
					h := hits[hi]
					fmt.Fprintf(&b, "vault-id\t%s\t%s\t%s\n", h.ID, h.Commit, h.Path)
				}
				for _, n := range diffNotes(dir, sha, short, signing, limit) {
					b.WriteString(n + "\n")
				}
			}
			if fi != len(found) || hi != len(hits) {
				return fmt.Errorf("diff-check: the range changed while it was checked")
			}
			for _, h := range specHits {
				fmt.Fprintf(&b, "spec-id\t%s\n", h)
			}
			fmt.Fprint(cmd.OutOrStdout(), b.String())
			if len(found) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "ct ws diff-check: %d file(s) flagged (binary or > %s)\n", len(found), maxSize)
			}
			if len(hits) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "ct ws diff-check: %d vault-only ID(s) of %s in the range\n", len(hits), ticket)
			}
			if len(specHits) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "ct ws diff-check: %d requirement ID(s) taken twice in specs/ (renumber the later one)\n", len(specHits))
			}
			if len(found) > 0 || len(hits) > 0 || len(specHits) > 0 {
				return exitError(3)
			}
			return nil
		},
	}
	cmd.SilenceUsage = true
	cmd.Flags().StringVarP(&dir, "dir", "C", "", "the git checkout to inspect (default: the current directory)")
	cmd.Flags().StringVar(&ticket, "ticket", "", "also flag this ticket's vault-only IDs (the ticket must be in the vault)")
	cmd.Flags().StringVar(&maxSize, "max-size", "1M", "size limit: bytes, or a number with K, M or G")
	return cmd
}

// diffNoteBool reports whether the boolean config key is true in the
// checkout's own config files (ct's git forces commit.gpgSign=false on its
// command line, which this skips): git normalises the values (a bare key
// is true) and the last one of the system, global, local and worktree
// files wins.
func diffNoteBool(dir, key string) bool {
	out, err := gitx.Out(dir, "config", "--show-scope", "--type=bool", "--get-all", key)
	if err != nil {
		return false
	}
	val := ""
	for _, l := range strings.Split(out, "\n") {
		scope, v, _ := strings.Cut(l, "\t")
		if scope != "command" && l != "" {
			val = strings.TrimSpace(v)
		}
	}
	return val == "true"
}

func diffNoteRegular(mode string) bool { return mode == "100644" || mode == "100755" }

// diffNotes returns the note lines of one commit: changed or removed
// tests against its first parent, then a missing signature when signing
// is configured. Read-only; any failure just yields fewer notes.
func diffNotes(dir, sha, short string, signing bool, limit int64) []string {
	var notes []string
	if parent, err := gitx.Out(dir, "rev-parse", "--verify", "-q", sha+"^1"); err == nil && parent != "" {
		out, err := gitx.OutIn(dir, nil, "diff-tree", "-r", "--no-renames", "--no-commit-id", "-z", "--raw", parent, sha)
		if err != nil {
			out = ""
		}
		toks := strings.Split(out, "\x00")
		for i := 0; i+1 < len(toks); i += 2 {
			meta := strings.Fields(strings.TrimPrefix(toks[i], ":"))
			p := toks[i+1]
			if len(meta) < 5 || !diffNoteRegular(meta[0]) {
				continue // added, or a gitlink or symlink before
			}
			oldSHA, newSHA, status := meta[2], meta[3], meta[4]
			if status != "M" && status != "D" && status != "T" {
				continue
			}
			line := func(name string) string {
				return fmt.Sprintf("note\ttest-changed\t%s\t%s\t%s", name, short, p)
			}
			switch {
			case strings.HasSuffix(p, ".txtar") && diffNoteInTestdata(p):
				notes = append(notes, line("-"))
			case strings.HasSuffix(p, "_test.go"):
				if status == "T" || (status == "M" && !diffNoteRegular(meta[1])) {
					notes = append(notes, line("-"))
					continue
				}
				for _, n := range diffNoteChangedTests(dir, oldSHA, newSHA, p, status == "D", limit) {
					notes = append(notes, line(n))
				}
			}
		}
	}
	if signing {
		raw, err := gitx.OutIn(dir, nil, "cat-file", "commit", sha)
		if err == nil {
			header, _, _ := strings.Cut(raw, "\n\n")
			signed := false
			for _, l := range strings.Split(header, "\n") {
				if strings.HasPrefix(l, "gpgsig ") || strings.HasPrefix(l, "gpgsig-sha256 ") {
					signed = true
				}
			}
			if !signed {
				notes = append(notes, fmt.Sprintf("note\tunsigned\t-\t%s\t:commit", short))
			}
		}
	}
	return notes
}

func diffNoteInTestdata(p string) bool {
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if seg == "testdata" {
			return true
		}
	}
	return false
}

// diffNoteChangedTests lists the test funcs of the old blob of a test
// file whose source text differs in the new blob or that it no longer has
// (all when deleted). "-" stands for the whole file when a blob is above
// limit, or the new one doesn't parse. Errors give no names.
func diffNoteChangedTests(dir, oldSHA, newSHA, p string, deleted bool, limit int64) []string {
	big := func(blob string) bool {
		out, err := gitx.Out(dir, "cat-file", "-s", blob)
		if err != nil {
			return false
		}
		n, err := strconv.ParseInt(out, 10, 64)
		return err == nil && n > limit
	}
	if big(oldSHA) || (!deleted && big(newSHA)) {
		return []string{"-"}
	}
	old, err := gitx.OutIn(dir, nil, "cat-file", "blob", oldSHA)
	if err != nil {
		return nil
	}
	before, ok := diffNoteTestFuncs(p, old)
	if !ok {
		return nil
	}
	var after diffNoteFuncs
	if !deleted {
		cur, err := gitx.OutIn(dir, nil, "cat-file", "blob", newSHA)
		if err != nil {
			return nil
		}
		if after, ok = diffNoteTestFuncs(p, cur); !ok {
			return []string{"-"}
		}
	}
	var names []string
	for _, n := range before.order {
		if txt, there := after.text[n]; !there || txt != before.text[n] {
			names = append(names, n)
		}
	}
	return names
}

// diffNoteIsTest mirrors go test's rule: the prefix, then not a lower
// case rune (Testdata is no test).
func diffNoteIsTest(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok {
			r, _ := utf8.DecodeRuneInString(rest)
			return rest == "" || !unicode.IsLower(r)
		}
	}
	return false
}

type diffNoteFuncs struct {
	order []string
	text  map[string]string
}

// diffNoteTestFuncs parses a Go file and returns its top-level test funcs
// (source order) with their source text; ok is false when it doesn't parse.
func diffNoteTestFuncs(name, src string) (diffNoteFuncs, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return diffNoteFuncs{}, false
	}
	res := diffNoteFuncs{text: map[string]string{}}
	for _, d := range f.Decls {
		fn, isFn := d.(*ast.FuncDecl)
		if !isFn || fn.Recv != nil {
			continue
		}
		n := fn.Name.Name
		if !diffNoteIsTest(n) {
			continue
		}
		start := fset.Position(fn.Pos()).Offset
		if fn.Doc != nil {
			start = fset.Position(fn.Doc.Pos()).Offset
		}
		res.order = append(res.order, n)
		res.text[n] = src[start:fset.Position(fn.End()).Offset]
	}
	return res, true
}

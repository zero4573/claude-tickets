package cli

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zero4573/tix/internal/cite"
	"github.com/zero4573/tix/internal/repo"
	"github.com/zero4573/tix/internal/vault"
)

func citeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cite",
		Short: "Check the code citations of vault notes",
	}
	cmd.AddCommand(citeCheckCmd())
	return cmd
}

func citeCheckCmd() *cobra.Command {
	var pinFlags, cloneFlags, pinsFrom []string
	var symbols bool
	cmd := &cobra.Command{
		Use:   "check [--pin <repo>=<rev>]... [--pins-from <note>]... [--clone <repo>=<dir>]... [--symbols] <note>...",
		Short: "Check a note's file:line citations and code chunks at the pinned commits",
		Long: `Reads each note (a path, or a vault note's name) and checks its code
citations at the commit the note pins for each repo, stated anywhere outside
code blocks as

  <repo> ` + "`<branch>`" + ` @ ` + "`<sha>`" + `      (the branch is optional)

or given with --pin <repo>=<rev>, which wins, or read from another note with
--pins-from <note> (e.g. the ticket's conventions.md), which the note's own
win over. A citation is

  - a caption line above a fenced code block (the chunk):
      **<repo>** · ` + "`<path>`" + ` · ` + "`<Symbol>`" + `, … · lines <N>-<M>
  - an inline reference in backticks, outside code blocks:
      ` + "`<path>:<N>`" + `, ` + "`<path>:<N>-<M>`" + ` or ` + "`<repo>:<path>:<N>[-<M>]`" + `

For each one it checks that the file is in the repo at the pinned commit (a
path that isn't may be the end of exactly one path there, e.g. ` + "`api.ex:63`" + `;
an inline reference without a repo is looked up in every pinned repo), and
that the lines are inside it; for a caption, that the chunk is the file's
text at those lines, verbatim but for each line's leading and trailing
whitespace (a chunk from inside a block may be dedented), where a line that is
just an ellipsis or a comment starting with one (// …, # ...) skips ahead;
with --symbols, that each backticked symbol of the caption (its last part:
` + "`Pkg.Func`" + ` is Func) appears in the lines (so a chunk from the middle of a
function fails it: the function's name is above the range).

A repo is found by --clone <repo>=<dir>, else among the vault's main clones
by slug or repo name (case, and _ against -, ignored). One line per failed
citation on stdout:

  <note>:<line>  <citation>  <reason>

Exits 0 when every citation holds, 3 when one fails, 1 on a usage error or a
note that can't be read.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pins, err := citePairs("--pin", pinFlags)
			if err != nil {
				return err
			}
			clones, err := citePairs("--clone", cloneFlags)
			if err != nil {
				return err
			}
			ctx, _ := vault.Optional()
			var base map[string]string
			for _, name := range pinsFrom {
				p, err := citeNotePath(ctx, name)
				if err != nil {
					return err
				}
				src, err := os.ReadFile(p)
				if err != nil {
					return fmt.Errorf("cite: %w", err)
				}
				if base == nil {
					base = map[string]string{}
				}
				for k, v := range cite.Parse(string(src)).Pins {
					base[k] = v
				}
			}
			c := &cite.Checker{Pins: pins, Symbols: symbols, Resolve: func(key string) (string, error) {
				return citeClone(ctx, clones, key)
			}}
			total, failed := 0, 0
			for _, name := range args {
				p, err := citeNotePath(ctx, name)
				if err != nil {
					return err
				}
				src, err := os.ReadFile(p)
				if err != nil {
					return fmt.Errorf("cite: %w", err)
				}
				n := cite.Parse(string(src))
				for k, v := range base {
					if _, own := n.Pins[k]; !own {
						n.Pins[k] = v
					}
				}
				total += len(n.Citations)
				for _, f := range c.Check(n) {
					failed++
					fmt.Fprintf(cmd.OutOrStdout(), "%s:%d\t%s\t%s\n", name, f.Line, f.Citation, citeOneLine(f.Reason))
				}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "tix cite check: %d citation(s), %d failed\n", total, failed)
			if failed > 0 {
				return exitError(3)
			}
			return nil
		},
	}
	cmd.SilenceUsage = true
	cmd.Flags().StringArrayVar(&pinFlags, "pin", nil, "the commit a repo is read at (<repo>=<rev>; wins over the note's)")
	cmd.Flags().StringArrayVar(&pinsFrom, "pins-from", nil, "also read pinned commits from this note (the note's own win)")
	cmd.Flags().StringArrayVar(&cloneFlags, "clone", nil, "where a repo is cloned (<repo>=<dir>)")
	cmd.Flags().BoolVar(&symbols, "symbols", false, "also check that a caption's symbols appear in its lines")
	return cmd
}

// citeOneLine keeps a failure on its line: a multi-line reason (git's
// refusal) is joined with "; ".
func citeOneLine(s string) string {
	var parts []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, "; ")
}

// citePairs reads repeated <repo>=<value> flags, keyed by cite.Key.
func citePairs(flag string, vals []string) (map[string]string, error) {
	out := map[string]string{}
	for _, v := range vals {
		k, val, ok := strings.Cut(v, "=")
		if !ok || strings.TrimSpace(k) == "" || val == "" {
			return nil, fmt.Errorf("cite: %s wants <repo>=<value>, got '%s'", flag, v)
		}
		out[cite.Key(k)] = val
	}
	return out, nil
}

// citeNotePath is a note given as a path, else as a vault note's name.
func citeNotePath(ctx vault.Context, name string) (string, error) {
	if st, err := os.Stat(name); err == nil && st.Mode().IsRegular() {
		return name, nil
	}
	if ctx.Vault == "" {
		return "", fmt.Errorf("cite: no file %s (and no vault to find a note of that name in)", name)
	}
	rel, err := resolveNote(ctx.Vault, name)
	if err != nil {
		return "", err
	}
	return filepath.Join(ctx.Vault, filepath.FromSlash(rel)), nil
}

// citeClone finds a repo's clone: a --clone dir, else the one main clone
// whose slug or repo name has the key.
func citeClone(ctx vault.Context, clones map[string]string, key string) (string, error) {
	if d, ok := clones[key]; ok {
		return d, nil
	}
	if ctx.ProjectsRoot == "" {
		return "", fmt.Errorf("no clone of %s (pass --clone %s=<dir>)", key, key)
	}
	var hits []string
	for _, c := range repo.MainClones(ctx.ProjectsRoot) {
		if cite.Key(repo.Slug(c)) == key || cite.Key(path.Base(c)) == key {
			hits = append(hits, c)
		}
	}
	switch len(hits) {
	case 1:
		return filepath.Join(ctx.ProjectsRoot, filepath.FromSlash(hits[0])), nil
	case 0:
		return "", fmt.Errorf("no main clone of %s under %s (pass --clone %s=<dir>)", key, ctx.ProjectsRoot, key)
	}
	return "", errors.New("several main clones are " + key + ": " + strings.Join(hits, ", ") + " (name its slug, or pass --clone)")
}

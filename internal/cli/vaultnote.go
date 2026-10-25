package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/links"
	"github.com/zero4573/claude-tickets/internal/mdsec"
	"github.com/zero4573/claude-tickets/internal/notesec"
	"github.com/zero4573/claude-tickets/internal/vault"
)

func init() { vaultSubcommands = append(vaultSubcommands, vaultNoteCmd) }

// afterNoteWrite runs after a note was written (vault, vault-relative
// path with slashes): the search index refreshes itself through it.
var afterNoteWrite = func(vault, rel string) {}

func vaultNoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "note",
		Short: "Read and edit a note by section or frontmatter field",
		Long: `Section-level access to a note, so an agent reads or changes the part it needs
and every other byte stays as it was. <note> is a bare name or a
vault-relative path (resolved like a wikilink); <section> is "Title",
"## Title" (level-checked) or "A > B".

  ct vault note outline <note> [--json]
  ct vault note get <note> [<section>] [--no-subsections]
  ct vault note set-section <note> <section> [--with-subsections] [--file f]
  ct vault note append <note> <section> [--create <level>] [--file f]
  ct vault note frontmatter <note> [<field>] [--json]
  ct vault note frontmatter <note> <field> --set <v> | --add <item> | --remove <item>

Content for writes comes from --file or stdin. Writes follow the vault's write
rules: in a ticket workspace only tickets/<ID>/ (never a ticket's identity
fields or content sections), outside one only inbox/; /tickets:save passes
--save for the shared notes. Exit codes: 0 ok, 1 error, 2 usage / not found /
ambiguous, 3 refused by a write rule.`,
	}
	cmd.AddCommand(noteOutlineCmd(), noteGetCmd(), noteSetSectionCmd(), noteAppendCmd(), noteFrontmatterCmd())
	return cmd
}

// noteErr maps a note error to ct's exit codes.
func noteErr(err error) error {
	var amb *mdsec.AmbiguousError
	var ref *notesec.RefusedError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ref):
		fmt.Fprintln(os.Stderr, "ct vault note: "+ref.Msg)
		return exitError(3)
	case errors.Is(err, mdsec.ErrNotFound), errors.As(err, &amb):
		return exitWith(2, err)
	}
	return err
}

func noteVault(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	return vault.Current()
}

// resolveNote finds the note a name or path stands for.
func resolveNote(v, name string) (rel string, err error) {
	hits := links.NewIndex(v).Resolve(strings.TrimSuffix(filepath.ToSlash(name), "#"))
	switch len(hits) {
	case 0:
		return "", exitWith(2, fmt.Errorf("note %q not found in %s", name, v))
	case 1:
		r, err := filepath.Rel(v, hits[0])
		if err != nil {
			return "", err
		}
		return filepath.ToSlash(r), nil
	}
	var paths []string
	for _, h := range hits {
		r, _ := filepath.Rel(v, h)
		paths = append(paths, filepath.ToSlash(r))
	}
	sort.Strings(paths)
	return "", exitWith(2, fmt.Errorf("note %q is ambiguous: %s", name, strings.Join(paths, ", ")))
}

type noteFlags struct {
	vault, file, owner string
	save               bool
}

func (f *noteFlags) read(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.vault, "vault", "", "the vault (default: the current one)")
}

func (f *noteFlags) write(cmd *cobra.Command) {
	f.read(cmd)
	cmd.Flags().BoolVar(&f.save, "save", false, "shared-notes mode (/tickets:save)")
	cmd.Flags().StringVar(&f.owner, "owner", "", "vault lock owner (default $CLAUDE_TICKETS_LOCK_OWNER, else ct-note-<pid>)")
}

func (f *noteFlags) content(cmd *cobra.Command) ([]byte, error) {
	if f.file != "" {
		return os.ReadFile(f.file)
	}
	return io.ReadAll(cmd.InOrStdin())
}

func (f *noteFlags) load(name string) (v, rel string, src []byte, err error) {
	if v, err = noteVault(f.vault); err != nil {
		return
	}
	if rel, err = resolveNote(v, name); err != nil {
		return
	}
	src, err = os.ReadFile(filepath.Join(v, filepath.FromSlash(rel)))
	return
}

// apply writes through the rules, the lock and the temp-file rename.
func (f *noteFlags) apply(cmd *cobra.Command, name string, op notesec.Op) error {
	v, err := noteVault(f.vault)
	if err != nil {
		return err
	}
	rel, err := resolveNote(v, name)
	if err != nil {
		return err
	}
	owner := f.owner
	if owner == "" {
		owner = os.Getenv("CLAUDE_TICKETS_LOCK_OWNER")
	}
	cwd, _ := os.Getwd()
	changed, err := notesec.Apply(notesec.Ctx{Vault: v, Workspace: notesec.WorkspaceID(cwd), Save: f.save}, rel, owner, op)
	if err != nil {
		return noteErr(err)
	}
	if !changed {
		fmt.Fprintf(cmd.OutOrStdout(), "unchanged %s\n", rel)
		return nil
	}
	afterNoteWrite(v, rel)
	fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", rel)
	return nil
}

func printJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	return enc.Encode(v)
}

func noteOutlineCmd() *cobra.Command {
	var f noteFlags
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "outline <note> [--json]",
		Short: "List a note's sections with their line ranges and sizes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, rel, src, err := f.load(args[0])
			if err != nil {
				return err
			}
			o := notesec.OutlineOf(src)
			if asJSON {
				return printJSON(cmd, struct {
					Path string `json:"path"`
					notesec.Outline
				}{rel, o})
			}
			w := cmd.OutOrStdout()
			if o.Frontmatter != nil {
				fmt.Fprintf(w, "%d-%d  frontmatter\n", o.Frontmatter.StartLine, o.Frontmatter.EndLine)
			}
			for _, s := range o.Sections {
				fmt.Fprintf(w, "%d-%d  %s %s  (~%d tok)\n", s.StartLine, s.EndLine, strings.Repeat("#", s.Level), s.Title, s.Tokens)
			}
			return nil
		},
	}
	f.read(cmd)
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return usageExit(cmd)
}

func noteGetCmd() *cobra.Command {
	var f noteFlags
	var noSub bool
	cmd := &cobra.Command{
		Use:   "get <note> [<section>] [--no-subsections]",
		Short: "Print a section (heading included), or the whole note",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, src, err := f.load(args[0])
			if err != nil {
				return err
			}
			spec := ""
			if len(args) > 1 {
				spec = args[1]
			}
			out, err := notesec.Get(src, spec, noSub)
			if err != nil {
				return noteErr(err)
			}
			_, err = cmd.OutOrStdout().Write(out)
			return err
		},
	}
	f.read(cmd)
	cmd.Flags().BoolVar(&noSub, "no-subsections", false, "stop at the section's first subsection")
	return usageExit(cmd)
}

func noteSetSectionCmd() *cobra.Command {
	var f noteFlags
	var withSub bool
	cmd := &cobra.Command{
		Use:   "set-section <note> <section> [--with-subsections] [--file f]",
		Short: "Replace a section's body (the heading line stays)",
		Long: `Replaces the section's body, by default only up to its first subsection, with
--with-subsections the subsections too. The new content ends with exactly one
newline; a blank line after the heading, and one before the next heading, are
kept if the original had them.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := f.content(cmd)
			if err != nil {
				return err
			}
			return f.apply(cmd, args[0], func(src []byte) ([]byte, notesec.Target, error) {
				return notesec.SetSection(src, args[1], content, withSub)
			})
		},
	}
	f.write(cmd)
	cmd.Flags().StringVar(&f.file, "file", "", "read the content from a file (default stdin)")
	cmd.Flags().BoolVar(&withSub, "with-subsections", false, "replace the subsections too")
	return usageExit(cmd)
}

func noteAppendCmd() *cobra.Command {
	var f noteFlags
	var create int
	cmd := &cobra.Command{
		Use:   "append <note> <section> [--create <level>] [--file f]",
		Short: "Append to the end of a section's own body",
		Long: `Appends before the section's first subsection. --create 2 adds a missing
section as "## <title>" at the end of the note instead of failing with exit 2.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := f.content(cmd)
			if err != nil {
				return err
			}
			return f.apply(cmd, args[0], func(src []byte) ([]byte, notesec.Target, error) {
				return notesec.Append(src, args[1], content, create)
			})
		},
	}
	f.write(cmd)
	cmd.Flags().StringVar(&f.file, "file", "", "read the content from a file (default stdin)")
	cmd.Flags().IntVar(&create, "create", 0, "heading level (1-6) to create a missing section with")
	return usageExit(cmd)
}

func noteFrontmatterCmd() *cobra.Command {
	var f noteFlags
	var asJSON bool
	var set, add, remove string
	cmd := &cobra.Command{
		Use:   "frontmatter <note> [<field>] [--json | --set v | --add item | --remove item]",
		Short: "Read or change frontmatter fields",
		Long: `Prints the fields, or one field's raw value. With --set (a scalar), --add or
--remove (list items, flow "[a, b]" and block "- a" styles kept) it changes
that field and nothing else.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			sets := 0
			for _, name := range []string{"set", "add", "remove"} {
				if cmd.Flags().Changed(name) {
					sets++
				}
			}
			if sets > 1 {
				return exitWith(2, errors.New("use only one of --set, --add and --remove"))
			}
			if sets == 1 {
				if len(args) != 2 {
					return exitWith(2, errors.New("--set, --add and --remove need a <field>"))
				}
				key := args[1]
				return f.apply(cmd, args[0], func(src []byte) ([]byte, notesec.Target, error) {
					var out []byte
					var err error
					switch {
					case cmd.Flags().Changed("set"):
						out, err = notesec.SetField(src, key, set)
					case cmd.Flags().Changed("add"):
						out, err = notesec.AddItem(src, key, add)
					default:
						out, err = notesec.RemoveItem(src, key, remove)
					}
					return out, notesec.Target{Field: key}, err
				})
			}
			_, _, src, err := f.load(args[0])
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(args) == 2 {
				raw, ok := notesec.Field(src, args[1])
				if !ok {
					return exitWith(2, fmt.Errorf("field %q not found", args[1]))
				}
				if asJSON {
					all, err := notesec.FieldsJSON(src)
					if err != nil {
						return err
					}
					return printJSON(cmd, map[string]any{args[1]: all[args[1]]})
				}
				fmt.Fprintln(w, raw)
				return nil
			}
			if asJSON {
				all, err := notesec.FieldsJSON(src)
				if err != nil {
					return err
				}
				return printJSON(cmd, all)
			}
			for _, kv := range notesec.Fields(src) {
				if kv.Raw == "" || strings.Contains(kv.Raw, "\n") {
					fmt.Fprintf(w, "%s:\n", kv.Key)
					if kv.Raw != "" {
						fmt.Fprintln(w, kv.Raw)
					}
					continue
				}
				fmt.Fprintf(w, "%s: %s\n", kv.Key, kv.Raw)
			}
			return nil
		},
	}
	f.write(cmd)
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	cmd.Flags().StringVar(&set, "set", "", "set a scalar field")
	cmd.Flags().StringVar(&add, "add", "", "add an item to a list field")
	cmd.Flags().StringVar(&remove, "remove", "", "remove an item from a list field")
	return usageExit(cmd)
}

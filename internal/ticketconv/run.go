package ticketconv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zero4573/tix/internal/gitx"
	"github.com/zero4573/tix/internal/jira"
	"github.com/zero4573/tix/internal/links"
	"github.com/zero4573/tix/internal/mcp"
	"github.com/zero4573/tix/internal/note"
	"github.com/zero4573/tix/internal/vaultlock"
	"github.com/zero4573/tix/internal/workspace"
)

// Options is one tix ticket convert.
type Options struct {
	Vault    string
	WorkRoot string
	ID       string // the manual ticket
	Source   string
	Project  string // --project; else the source's "project" in .sources.json
	Type     string // --type; else mapped from the note's ticket-type
	DryRun   bool
	Out      io.Writer
}

// PartialError is a convert that stopped after the issue was filed: Key is
// the new ID and the message says what was done and how to finish.
type PartialError struct {
	Key string
	Msg string
}

func (e *PartialError) Error() string { return e.Msg }

type sourceConf struct {
	Enabled *bool  `json:"enabled"`
	Sync    *bool  `json:"sync"`
	MCP     string `json:"mcp"`
	Site    string `json:"site"`
	Project string `json:"project"`
}

func readSource(vault, source string) (sourceConf, error) {
	var sc sourceConf
	cfgFile := filepath.Join(vault, "tickets", ".sources.json")
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		return sc, fmt.Errorf("no %s (it lists the ticket sources)", cfgFile)
	}
	var cfg struct {
		Sources map[string]sourceConf `json:"sources"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return sc, fmt.Errorf("%s: %w", cfgFile, err)
	}
	sc, ok := cfg.Sources[source]
	switch {
	case !ok:
		return sc, fmt.Errorf("no source '%s' in %s", source, cfgFile)
	case sc.Enabled != nil && !*sc.Enabled:
		return sc, fmt.Errorf("source '%s' is disabled in %s", source, cfgFile)
	case sc.Sync != nil && !*sc.Sync:
		return sc, fmt.Errorf("source '%s' has \"sync\": false: a ticket can only be converted to a synced source", source)
	case sc.MCP == "":
		return sc, fmt.Errorf("source '%s' has no \"mcp\" server set in %s", source, cfgFile)
	}
	return sc, nil
}

// Seams for the failure tests: the writes of the note and the vault move.
var (
	writeFile  = os.WriteFile
	renameTree = links.RenameTree
)

// findConverted is the folder of a ticket whose vault move is done but
// whose workspace step isn't: the one under source recording converted-from.
func findConverted(vault, source, oldID string) (id string) {
	ents, _ := os.ReadDir(note.SourceDir(vault, source))
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(note.SourceDir(vault, source), e.Name(), e.Name()+".md"))
		if err != nil {
			continue
		}
		text := string(data)
		if Field(text, "converted-from") == oldID {
			return e.Name()
		}
		// The note moved but its record didn't get written: still the manual
		// ticket's note, filed as this folder's key
		if Field(text, "source") == "manual" && Field(text, "convert-source") == source && Field(text, "convert-key") == e.Name() {
			if sid := Field(text, "source-id"); sid == "" || sid == oldID {
				return e.Name()
			}
		}
	}
	return ""
}

// Run converts the manual ticket o.ID into an issue at o.Source. Refusals
// come first and change nothing; the issue is created before anything local
// changes; a failure after that is a *PartialError, and running it again
// (same arguments) finishes the job without filing a second issue.
func Run(o Options) error {
	out := o.Out
	if out == nil {
		out = io.Discard
	}
	o.Out = out
	if !note.ValidKey(o.ID) {
		return fmt.Errorf("'%s' is not a ticket ID", o.ID)
	}
	if !note.ValidSource(o.Source) {
		return fmt.Errorf("'%s' is not a source name", o.Source)
	}
	if o.Source == "manual" {
		return errors.New("'manual' is not a synced source: use --to <source> (e.g. jira)")
	}
	oldDir := filepath.Join(o.WorkRoot, o.ID)
	if workspace.Running(oldDir) {
		return fmt.Errorf("%s's session is running: close it first (tix status)", o.ID)
	}
	sc, err := readSource(o.Vault, o.Source)
	if err != nil {
		return err
	}
	if !o.DryRun {
		// Convert rewrites links across the vault and moves a folder: not beside a save
		const owner = "tix ticket convert"
		if err := vaultlock.Acquire(o.Vault, owner); err != nil {
			return fmt.Errorf("couldn't take the vault lock, nothing changed: %w", err)
		}
		defer vaultlock.Release(o.Vault, owner)
	}
	notePath := note.Find(o.Vault, o.ID)
	if notePath == "" {
		// Resume: the vault move is done, the workspace step isn't
		if key := findConverted(o.Vault, o.Source, o.ID); key != "" {
			fmt.Fprintf(out, "%s was already filed and moved as %s: finishing\n", o.ID, key)
			if left := filepath.Join(o.Vault, "tickets", "manual", o.ID); dirExists(left) {
				// a split folder: finish moving what's left
				if err := renameTree(o.Vault, left, filepath.Join(note.SourceDir(o.Vault, o.Source), key), RenameFile(o.ID, key), o.ID+".md"); err != nil {
					return partial(o, key, "filed and partly moved, but the vault move failed", err)
				}
			}
			return finish(o, key, "")
		}
		return fmt.Errorf("no ticket %s in the vault", o.ID)
	}
	data, err := os.ReadFile(notePath)
	if err != nil {
		return err
	}
	text := string(data)
	if note.SourceOf(o.Vault, o.ID) != "manual" || Field(text, "source") != "manual" {
		return fmt.Errorf("%s is not a manual ticket (source %s)", o.ID, note.SourceOf(o.Vault, o.ID))
	}

	key, url := Field(text, "convert-key"), Field(text, "convert-url")
	if key != "" {
		if s := Field(text, "convert-source"); s != o.Source {
			return fmt.Errorf("%s was already filed at '%s' as %s: run it again with --to %s", o.ID, s, key, s)
		}
		fmt.Fprintf(out, "%s was already filed at %s as %s: not filing it again\n", o.ID, o.Source, key)
		if o.DryRun {
			fmt.Fprintln(out, "dry run: nothing changed")
			return nil
		}
	} else {
		in, w, err := prepare(o, sc, text)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s -> %s\n  project:  %s\n  type:     %s\n  summary:  %s\n  assignee: you (%s)\n", o.ID, o.Source, in.Project, in.Type, in.Summary, in.Assignee)
		if in.Parent != "" {
			fmt.Fprintf(out, "  parent:   %s\n", in.Parent)
		}
		fmt.Fprintf(out, "  description:\n%s\n", indent(in.Description, "    "))
		if o.DryRun {
			fmt.Fprintln(out, "dry run: nothing filed, nothing changed")
			return nil
		}
		if key, url, err = w.Create(in); err != nil {
			var unsure *jira.UnsureError
			if errors.As(err, &unsure) {
				return fmt.Errorf("couldn't confirm that %s was filed at %s: %w\nthe issue may exist: check the %s project for it before running this again (a re-run would file a second issue); if it does, set 'convert-source: %s', 'convert-key: <its key>' and 'convert-url: <its URL>' in the note's frontmatter, then run it again", o.ID, o.Source, err, in.Project, o.Source)
			}
			return fmt.Errorf("couldn't file %s at %s (nothing changed): %w", o.ID, o.Source, err)
		}
		fmt.Fprintf(out, "filed %s at %s: %s\n", key, o.Source, url)
		if !note.ValidKey(key) {
			return &PartialError{key, fmt.Sprintf("%s was filed, but the source answered '%q', which is not a valid ticket ID: nothing changed; find the issue at %s, then set 'convert-source: %s', 'convert-key' and 'convert-url' in the note's frontmatter and run it again", o.ID, key, o.Source, o.Source)}
		}
		rec := SetFields(text, "convert-source", o.Source, "convert-key", key, "convert-url", url)
		if err := writeFile(notePath, []byte(rec), 0o644); err != nil {
			return &PartialError{key, fmt.Sprintf("filed %s as %s (%s) but couldn't record it in %s: %v\nnothing else changed; to finish, run: tix ticket convert %s --to %s\n(it would file a second issue: first set 'convert-source: %s', 'convert-key: %s' and 'convert-url: %s' in the note's frontmatter)", o.ID, key, url, notePath, err, o.ID, o.Source, o.Source, key, url)}
		}
	}
	if !note.ValidKey(key) {
		return &PartialError{key, fmt.Sprintf("%s was filed as '%s', which is not a valid ticket ID: nothing else changed; fix convert-key in the note", o.ID, key)}
	}
	// The resume record goes in while the note is still in its old folder: a
	// stop anywhere in the move leaves a state a re-run finds and finishes
	if cur, err := os.ReadFile(notePath); err == nil && Field(string(cur), "converted-from") != o.ID {
		if err := writeFile(notePath, []byte(SetFields(string(cur), "converted-from", o.ID)), 0o644); err != nil {
			return partial(o, key, "filed, but its resume record couldn't be written", err)
		}
	}
	if err := moveVault(o, notePath, key, url); err != nil {
		return partial(o, key, "filed it, but the vault move failed", err)
	}
	return finish(o, key, url)
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func partial(o Options, key, what string, err error) error {
	return &PartialError{key, fmt.Sprintf("%s was %s: %v\ndone: the issue %s exists at %s\nto finish, run again: tix ticket convert %s --to %s", o.ID, what, err, key, o.Source, o.ID, o.Source)}
}

func indent(s, p string) string {
	if s == "" {
		return p + "(none)"
	}
	return p + strings.ReplaceAll(s, "\n", "\n"+p)
}

// prepare is the create step up to the call: what to file and the connected
// writer. It talks to the source's MCP server (reads only).
func prepare(o Options, sc sourceConf, text string) (jira.NewIssue, *jira.Writer, error) {
	var in jira.NewIssue
	in.Project = o.Project
	if in.Project == "" {
		in.Project = sc.Project
	}
	if in.Project == "" {
		return in, nil, fmt.Errorf("no project to file in: pass --project <KEY>, or set \"project\" on the %s source in tickets/.sources.json", o.Source)
	}
	in.Type = o.Type
	if in.Type == "" {
		if in.Type = jira.TypeFor(Field(text, "ticket-type")); in.Type == "" {
			return in, nil, fmt.Errorf("no default issue type for ticket-type '%s': pass --type", Field(text, "ticket-type"))
		}
	}
	in.Summary = Field(text, "summary")
	if in.Summary == "" {
		return in, nil, fmt.Errorf("%s has no summary: set it in the note", o.ID)
	}
	in.Description = Description(text)
	c, err := mcp.Connect(sc.MCP)
	if err != nil {
		return in, nil, err
	}
	w := jira.NewWriter(c, sc.Site)
	if err := w.Connect(); err != nil {
		return in, nil, err
	}
	if err := w.CheckType(in.Project, in.Type); err != nil {
		return in, nil, err
	}
	in.Assignee = w.Me()
	// The parent: a synced epic that covers this ticket
	if cb := strings.Trim(Field(text, "covered-by"), "[] "); cb != "" && note.ValidKey(cb) {
		if p := note.Find(o.Vault, cb); p != "" && note.SourceOf(o.Vault, cb) != "manual" && note.Get(p, "ticket-type") == "epic" {
			in.Parent = cb
		}
	}
	return in, w, nil
}

// moveVault moves the ticket's folder to tickets/<source>/<key>/ (files
// renamed, links across the vault following), turns the note synced and
// rewrites the ID inside the folder.
func moveVault(o Options, notePath, key, url string) error {
	src := filepath.Dir(notePath)
	dst := filepath.Join(note.SourceDir(o.Vault, o.Source), key)
	if err := renameTree(o.Vault, src, dst, RenameFile(o.ID, key), filepath.Base(notePath)); err != nil {
		return err
	}
	fmt.Fprintf(o.Out, "moved %s to %s\n", filepath.ToSlash(relTo(o.Vault, src)), filepath.ToSlash(relTo(o.Vault, dst)))
	return flipFolder(o, key, url, dst)
}

func relTo(base, p string) string {
	if r, err := filepath.Rel(base, p); err == nil {
		return r
	}
	return p
}

// flipFolder turns the moved note synced and rewrites the old ID to the new
// one in every text file of the folder.
func flipFolder(o Options, key, url, dst string) error {
	np := filepath.Join(dst, key+".md")
	data, err := os.ReadFile(np)
	if err != nil {
		return err
	}
	if url == "" {
		if url = Field(string(data), "source-url"); url == "" {
			url = Field(string(data), "convert-url")
		}
	}
	if err := writeFile(np, []byte(Flip(string(data), o.Source, key, url, o.ID, Label(o.Source))), 0o644); err != nil {
		return err
	}
	return filepath.WalkDir(dst, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			return err
		}
		if n := RewriteID(string(b), o.ID, key); n != string(b) {
			return writeFile(p, []byte(n), 0o644)
		}
		return nil
	})
}

// finish is the rest after the vault move: the ID inside the folder (again
// on a resume), the workspace, and dropping the converted-from record.
func finish(o Options, key, url string) error {
	dst := filepath.Join(note.SourceDir(o.Vault, o.Source), key)
	if url == "" { // resumed: the note is flipped already; run the rewrite again
		if err := flipFolder(o, key, "", dst); err != nil {
			return partial(o, key, "moved in the vault, but rewriting its ID failed", err)
		}
	}
	if err := workspaceStep(o, key); err != nil {
		return partial(o, key, "moved in the vault, but its workspace wasn't renamed", err)
	}
	np := filepath.Join(dst, key+".md")
	if data, err := os.ReadFile(np); err == nil {
		if err := writeFile(np, []byte(SetFields(string(data), "converted-from", "")), 0o644); err != nil {
			fmt.Fprintf(o.Out, "warning: couldn't drop 'converted-from' from %s: %v\n  remove it by hand; until then tix ticket convert %s would run the workspace step again (harmless)\n", np, err, o.ID)
		}
	}
	fmt.Fprintf(o.Out, "converted %s to %s\n", o.ID, key)
	fmt.Fprintf(o.Out, "the branch keeps its name; the session's conversation starts fresh: tix start --continue %s finds none (Claude keeps transcripts per directory)\n", key)
	fmt.Fprintf(o.Out, "next: tix sync --source %s fills %s's source block, then tix start %s\n", o.Source, key, key)
	return nil
}

// workspaceStep renames the workspace directory, rewrites its
// workspace.json and repairs the worktrees' links with their main clones.
func workspaceStep(o Options, key string) error {
	oldDir, newDir := filepath.Join(o.WorkRoot, o.ID), filepath.Join(o.WorkRoot, key)
	if workspace.Running(newDir) {
		return fmt.Errorf("%s's session is running: close it first (tix status), then run tix ticket convert again", key)
	}
	_, oldErr := os.Stat(oldDir)
	_, newErr := os.Stat(newDir)
	switch {
	case oldErr == nil && newErr == nil:
		return fmt.Errorf("both %s and %s exist: merge them by hand, then finish with tix ticket convert", oldDir, newDir)
	case oldErr == nil:
		if err := os.Rename(oldDir, newDir); err != nil {
			return err
		}
		fmt.Fprintf(o.Out, "renamed workspace %s to %s\n", oldDir, newDir)
	case newErr != nil:
		fmt.Fprintf(o.Out, "no workspace for %s\n", o.ID)
		return nil
	}
	wf := workspace.File(newDir)
	if _, err := os.Stat(wf); err == nil {
		// workspace.json may spell the work root another way (a symlink, a
		// trailing separator): compare both sides resolved
		resolvedOld := evalLoose(oldDir)
		if err := workspace.Update(wf, func(doc map[string]any) error {
			RewriteWorkspace(doc, key, oldDir, newDir)
			if repos, ok := doc["repos"].([]any); ok {
				for _, r := range repos {
					if m, ok := r.(map[string]any); ok {
						p, _ := m["path"].(string)
						if np, ok := Reparent(evalLoose(p), resolvedOld, newDir); ok {
							m["path"] = np
						}
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	var paths []string
	if data, err := os.ReadFile(wf); err == nil {
		// raw, not workspace.Read: that one drops a path outside the workspace, the very
		// stale entry this step must report
		var raw struct {
			Repos []struct {
				Path string `json:"path"`
			} `json:"repos"`
		}
		if json.Unmarshal(data, &raw) == nil {
			for _, r := range raw.Repos {
				if r.Path != "" {
					paths = append(paths, r.Path)
				}
			}
		}
	}
	tasks, _ := os.ReadDir(filepath.Join(newDir, "tasks"))
	for _, t := range tasks {
		if t.IsDir() {
			paths = append(paths, filepath.Join(newDir, "tasks", t.Name()))
		}
	}
	var bad []string
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			bad = append(bad, fmt.Sprintf("%s: %v (workspace.json still names a path that isn't there: fix it by hand)", p, err))
			continue
		}
		g := MainGitDir(p)
		if g == "" {
			continue
		}
		if err := gitx.Run(g, "worktree", "repair", p); err != nil {
			bad = append(bad, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		fmt.Fprintf(o.Out, "repaired worktree %s\n", p)
	}
	if len(bad) > 0 {
		return fmt.Errorf("worktree step failed:\n  %s", strings.Join(bad, "\n  "))
	}
	return nil
}

// evalLoose is p with symlinks resolved as far as it exists: the longest
// existing prefix resolved, the rest appended.
func evalLoose(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

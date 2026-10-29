package ticketconv

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/vaultlock"
)

// StubOptions is a ticket just filed at its source, under a lead ticket.
type StubOptions struct {
	Vault     string
	ID        string // the new ticket's ID at the source
	Source    string // the synced source it was filed at
	Summary   string
	CoveredBy string // the lead ticket, whose note lists the new one in covers
	Today     string // YYYY-MM-DD; default: today
	Out       io.Writer
}

// yamlScalar is s as a plain frontmatter value, double quoted when a plain
// one would not read back as the same text.
func yamlScalar(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	if strings.ContainsAny(s, "\"\\#") || strings.Contains(s, ": ") || strings.HasSuffix(s, ":") ||
		strings.ContainsAny(s[:1], "-?:,[]{}&*!|>'%@`") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
	}
	return s
}

// Stub writes the note of a ticket filed at a synced source in the synced
// layout, with an empty source block for ct sync to fill (it finds the note
// by ID), covered-by the lead, and, for a manual lead, adds the ticket to the
// lead's covers; a synced lead's covers belong to its source, so they are left
// alone. It refuses an existing note and a closed lead, and changes nothing on
// any refusal.
func Stub(o StubOptions) error {
	if !note.ValidKey(o.ID) {
		return fmt.Errorf("'%s' is not a ticket ID", o.ID)
	}
	if !note.ValidKey(o.CoveredBy) {
		return fmt.Errorf("'%s' is not a ticket ID", o.CoveredBy)
	}
	if o.ID == o.CoveredBy {
		return errors.New("a ticket can't cover itself")
	}
	if !note.ValidSource(o.Source) {
		return fmt.Errorf("'%s' is not a source name", o.Source)
	}
	if o.Source == "manual" {
		return errors.New("'manual' is not a synced source: only a ticket filed at one gets a stub")
	}
	if _, err := readSource(o.Vault, o.Source); err != nil {
		return err
	}
	if strings.TrimSpace(o.Summary) == "" {
		return errors.New("--summary is required")
	}
	const owner = "ct ticket stub"
	if err := vaultlock.Acquire(o.Vault, owner); err != nil {
		return fmt.Errorf("couldn't take the vault lock, nothing changed: %w", err)
	}
	defer vaultlock.Release(o.Vault, owner)

	if p := note.Find(o.Vault, o.ID); p != "" {
		return fmt.Errorf("%s already has a note (%s): not overwriting it", o.ID, p)
	}
	lead := note.Find(o.Vault, o.CoveredBy)
	if lead == "" {
		return fmt.Errorf("no note for %s, the lead", o.CoveredBy)
	}
	if st := strings.ToLower(note.Get(lead, "status")); st == "closed" || st == "done" {
		return fmt.Errorf("the lead %s is %s: not covering a new ticket under it", o.CoveredBy, st)
	}
	synced := false
	if src := note.Get(lead, "source"); src != "" && src != "manual" {
		synced = true
	}
	if o.Out != nil {
		if typ := note.Get(lead, "ticket-type"); !strings.EqualFold(typ, "epic") {
			fmt.Fprintf(o.Out, "warning: %s is not an epic (ticket-type %q): it is used as the lead anyway\n", o.CoveredBy, typ)
		}
	}
	tpl, err := os.ReadFile(filepath.Join(o.Vault, "templates", "ticket-synced.md"))
	if err != nil {
		return err
	}
	today := o.Today
	if today == "" {
		today = time.Now().Format("2006-01-02")
	}
	text := strings.ReplaceAll(strings.ReplaceAll(string(tpl), "{{title}}", o.ID), "{{date}}", today)
	fm, rest, ok := frontmatter(text)
	if !ok {
		return errors.New("templates/ticket-synced.md has no frontmatter")
	}
	project := strings.ToLower(strings.SplitN(o.ID, "-", 2)[0])
	fm = setKey(fm, "summary", yamlScalar(o.Summary), false)
	fm = setKey(fm, "source", o.Source, false)
	fm = setKey(fm, "covered-by", `"[[`+o.CoveredBy+`]]"`, false)
	fm = editTags(fm, "", o.Source, project)
	text = "---\n" + strings.Join(fm, "") + rest

	file := note.SourcePath(o.Vault, o.Source, o.ID)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(text)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && !synced {
		err = addCovers(lead, o.ID)
	}
	if err != nil {
		_ = os.Remove(file)
		_ = os.Remove(filepath.Dir(file))
		return err
	}
	if o.Out != nil {
		fmt.Fprintf(o.Out, "%s: stub note written, covered by %s\n", o.ID, o.CoveredBy)
		if synced {
			fmt.Fprintf(o.Out, "%s is synced from %s: its covers are left to the source; only %s's covered-by was written\n", o.CoveredBy, note.Get(lead, "source"), o.ID)
		}
	}
	return nil
}

// addCovers lists id in the covers of the lead's note, once.
func addCovers(lead, id string) error {
	cur := note.ListField(lead, "covers")
	var items []string
	for _, c := range cur {
		if strings.Trim(c, "[]") == id {
			return nil
		}
		items = append(items, `"`+c+`"`)
	}
	items = append(items, `"[[`+id+`]]"`)
	return note.Set(lead, "covers", "["+strings.Join(items, ", ")+"]")
}

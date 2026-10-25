package notesec

import (
	"path"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/zero4573/claude-tickets/internal/mdsec"
)

// checkWrite runs the target rules, then, for a ticket note, checks what
// the write actually changes: the target a write names is not enough (a
// set-section of the note's H1 with its subsections names only the H1).
func checkWrite(c Ctx, rel string, src, out []byte, t Target) error {
	covered, err := check(c, rel, t)
	if err != nil {
		return err
	}
	if _, ok := ticketOf(path.Clean(filepath.ToSlash(rel))); !ok {
		return nil
	}
	if err := checkContent(rel, t); err != nil {
		return err
	}
	if !reflect.DeepEqual(protectedView(src, covered), protectedView(out, covered)) {
		what := "the Description, Acceptance criteria, Context / links or identity fields"
		if covered {
			what = "anything but the work sections and status"
		}
		if t.Level == 1 {
			return refuse("refusing to rewrite the H1 of %s with its subsections: that reaches %s of a ticket note, which agents never write (AGENTS.md, \"Who writes what\")", rel, what)
		}
		return refuse("refusing to write %s: the change reaches %s of a ticket note, which agents never write (AGENTS.md, \"Who writes what\")", rel, what)
	}
	return nil
}

// checkContent refuses content that brings headings the section's own
// structure can't hold: a heading at or above the section's level re-parents
// what follows, and one titled like a content section adds a second one.
func checkContent(rel string, t Target) error {
	if len(t.Content) == 0 || len(t.Section) == 0 {
		return nil
	}
	// a leading blank line keeps a "---" first line from reading as frontmatter
	for _, s := range mdsec.Parse(append([]byte("\n"), t.Content...)).Sections {
		if s.Level == 0 {
			continue
		}
		if t.Level > 0 && s.Level <= t.Level {
			if t.Level == 1 {
				return refuse("refusing to rewrite the H1 of %s: the content has a level-%d heading %q, and a rewrite of the H1 with its subsections would have to carry the note's protected sections (Description, Acceptance criteria, Context / links), which agents never write", rel, s.Level, s.Title)
			}
			return refuse("refusing to write section %q of %s: the content has a level-%d heading %q, at or above the section's level %d", strings.Join(t.Section, " > "), rel, s.Level, s.Title, t.Level)
		}
		if contentSections[strings.ToLower(strings.TrimSpace(s.Title))] {
			return refuse("refusing to write section %q of %s: the content has a heading titled %q, like a protected section", strings.Join(t.Section, " > "), rel, s.Title)
		}
	}
	return nil
}

// protectedView is the parts of a ticket note agents may not change, as
// comparable strings: the content sections and identity fields, or, for a
// covered ticket, everything outside the work sections and status.
func protectedView(src []byte, covered bool) []string {
	d := mdsec.Parse(src)
	var view []string
	for _, kv := range Fields(src) {
		if covered {
			if strings.ToLower(kv.Key) != "status" {
				view = append(view, "F:"+kv.Key+"="+kv.Raw)
			}
		} else if protectedField(kv.Key) {
			view = append(view, "F:"+kv.Key+"="+kv.Raw)
		}
	}
	if !covered {
		for _, s := range d.Sections {
			if s.Level > 0 && contentSections[strings.ToLower(strings.TrimSpace(s.Title))] {
				view = append(view, "S:"+string(src[s.Whole.Start:s.Whole.End]))
			}
		}
		return view
	}
	// everything after the frontmatter except the work sections
	pos := 0
	if d.Frontmatter != nil {
		pos = d.Frontmatter.End
	}
	var rest strings.Builder
	for _, s := range d.Sections {
		if s.Level == 0 || !workSections[strings.ToLower(strings.TrimSpace(s.Title))] || s.Whole.Start < pos {
			continue
		}
		rest.Write(src[pos:s.Whole.Start])
		pos = s.Whole.End
	}
	rest.Write(src[pos:])
	return append(view, "B:"+rest.String())
}

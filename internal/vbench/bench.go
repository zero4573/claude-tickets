package vbench

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zero4573/claude-tickets/internal/chunk"
	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/notesec"
	"github.com/zero4573/claude-tickets/internal/vsearch"
)

// Budget is the `--budget` the NEW kb strategy searches with.
const Budget = 2000

// RecallBudget is the `--budget` the recall skill's step 0 searches with (its
// SKILL.md says the same number): recall reads the logs and the project note
// next, so the search only has to point at what matters.
const RecallBudget = 1000

// ReadN is how many whole notes the OLD kb strategy reads: the best-matching ones.
const ReadN = 3

// Outcome is one strategy's result for one question.
type Outcome struct {
	Tokens int
	Found  bool // the planted sentence is in the context
}

// listing counts what `ls` of the folders a skill lists prints.
func listing(vault string, folders ...string) string {
	var b strings.Builder
	for _, f := range folders {
		es, _ := os.ReadDir(filepath.Join(vault, f))
		fmt.Fprintf(&b, "%s:\n", f)
		for _, e := range es {
			b.WriteString(e.Name() + "\n")
		}
	}
	return b.String()
}

// mdFiles lists the vault's notes in path order, skipping dot-dirs.
func mdFiles(vault string) []string {
	var out []string
	_ = filepath.WalkDir(vault, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && p != vault {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") {
			rel, _ := filepath.Rel(vault, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func read(vault, rel string) string {
	b, _ := os.ReadFile(filepath.Join(vault, filepath.FromSlash(rel)))
	return string(b)
}

var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "to": true, "for": true, "on": true, "in": true, "is": true, "are": true,
	"was": true, "what": true, "which": true, "who": true, "how": true, "does": true, "do": true, "did": true, "and": true,
	"or": true, "by": true, "at": true, "before": true, "right": true, "now": true, "last": true, "still": true,
}

// Distinctive is what an agent greps for: the fact's keywords without project
// names and stop words.
func Distinctive(f Fact) []string {
	keys, _ := DistinctiveKeys(f)
	return keys
}

// DistinctiveKeys is Distinctive plus whether it fell back to all the keywords
// because none was distinctive (the OLD strategy then greps for common words,
// which inflates its cost; W6 C01).
func DistinctiveKeys(f Fact) (keys []string, fallback bool) {
	proj := map[string]bool{}
	for _, p := range projectSlugs {
		proj[p] = true
	}
	var out []string
	for _, k := range f.Keywords {
		k = strings.ToLower(k)
		if !proj[k] && !stopWords[k] {
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		fallback = true
		for _, k := range f.Keywords {
			out = append(out, strings.ToLower(k))
		}
	}
	return out, fallback
}

// FallbackNote is the report line naming the facts whose OLD grep used all
// keywords for want of a distinctive one; "" when there are none.
func FallbackNote(facts []Fact) string {
	var qs []string
	for _, f := range facts {
		if _, fb := DistinctiveKeys(f); fb {
			qs = append(qs, f.Question)
		}
	}
	if len(qs) == 0 {
		return ""
	}
	return fmt.Sprintf("old: %d fact(s) had no distinctive keyword, so the grep used all keywords (marked \"*\"): %s", len(qs), strings.Join(qs, "; "))
}

// Old is the strategy before vault search (W4 S01). kb: ls of the skill's
// folders, then a case-insensitive grep -l of the distinctive keywords; the hits
// are ranked by how many distinct keywords they contain and the top ReadN are
// read whole (an agent would read the best matches, not the first alphabetically).
// recall: the old recall skill's steps as written before task 08
// (1cf4eb1:assets/plugin/skills/recall/SKILL.md): the 3 latest project logs,
// the decision notes (the generated vaults have no decisions index, so all of
// them stand in for it and the linked notes), the project note and the ticket
// notes that list the project; for a ticket question its note and board.
func Old(vault string, files []string, f Fact) Outcome {
	var ctx strings.Builder
	if f.Scenario == "kb" {
		ctx.WriteString(listing(vault, "projects", "knowledge-base", "references"))
		type hit struct {
			rel string
			n   int
		}
		var hits []hit
		keys := Distinctive(f)
		for _, rel := range files {
			low := strings.ToLower(read(vault, rel))
			n := 0
			for _, k := range keys {
				if strings.Contains(low, k) {
					n++
				}
			}
			if n > 0 {
				hits = append(hits, hit{rel, n})
			}
		}
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].n > hits[j].n })
		for _, h := range hits {
			ctx.WriteString(h.rel + "\n")
		}
		for i := 0; i < len(hits) && i < ReadN; i++ {
			ctx.WriteString(read(vault, hits[i].rel))
		}
	} else {
		pre := "projects/" + f.Project + "/"
		ctx.WriteString(listing(vault, "projects", "projects/"+f.Project))
		var logs []string
		for _, rel := range files {
			switch {
			case strings.HasPrefix(rel, pre):
				switch {
				case strings.Contains(rel, "-log-"):
					logs = append(logs, rel)
				case strings.Contains(rel, "-decision-") || rel == pre+f.Project+".md":
					ctx.WriteString(read(vault, rel))
				}
			case isTicketNote(rel):
				// a ticket's own note: open tickets that list the project
				if b := read(vault, rel); listsProject(b, f.Project) {
					ctx.WriteString(b)
				}
			}
		}
		for i := max(0, len(logs)-3); i < len(logs); i++ {
			ctx.WriteString(read(vault, logs[i]))
		}
		if f.Ticket != "" {
			ctx.WriteString(listing(vault, ticketDir(f.Ticket)))
			ctx.WriteString(read(vault, ticketDir(f.Ticket)+"/"+f.Ticket+".md"))
			ctx.WriteString(read(vault, ticketDir(f.Ticket)+"/"+f.Ticket+"-board.md"))
		}
	}
	s := ctx.String()
	return Outcome{Tokens: chunk.Tokens(s), Found: strings.Contains(s, f.Sentence)}
}

// Render is what `ct vault search --budget` prints to the agent for a response.
func Render(r *vsearch.Response) string {
	var b strings.Builder
	for _, x := range r.Results {
		fmt.Fprintf(&b, "%d. %s › %s  (%.2f)\n%s\n", x.Rank, x.Path, strings.Join(x.Heading, " > "), x.Score, x.Text)
	}
	return b.String()
}

// New is what the new skills do (W4 S02): one `search --k 30 --budget 2000`
// call (recall questions filter by project, the ticket's folder for a ticket
// question), then, when the search text doesn't contain the answer, a
// `note get <top note> "<top section>"`, whose tokens count too. FollowUp
// reports whether that second call was needed.
func New(vault string, r *vsearch.Response, f Fact) (o Outcome, followUp bool) {
	s := Render(r)
	o = Outcome{Tokens: chunk.Tokens(s), Found: strings.Contains(s, f.Sentence)}
	if o.Found || len(r.Results) == 0 {
		return o, false
	}
	top := r.Results[0]
	if len(top.Heading) == 0 {
		return o, true
	}
	sec := top.Heading[len(top.Heading)-1:]
	got, err := notesec.Get([]byte(read(vault, top.Path)), strings.Join(sec, ""), false)
	if err != nil && len(top.Heading) > 1 {
		got, err = notesec.Get([]byte(read(vault, top.Path)), strings.Join(top.Heading, " > "), false)
	}
	if err != nil {
		return o, true // the call is made, but gives nothing to read
	}
	o.Tokens += chunk.Tokens(string(got))
	o.Found = strings.Contains(string(got), f.Sentence)
	return o, true
}

// SearchOptions are the vsearch options for a fact's question.
func SearchOptions(f Fact) vsearch.Options {
	o := vsearch.Options{K: 30, Budget: Budget}
	if f.Scenario == "recall" {
		o.Budget = RecallBudget
		o.Project = f.Project
		if f.Ticket != "" {
			// the skill's ticket step: search the ticket's own folder, and
			// its board for what is open
			o.Project = ""
			o.Paths = []string{ticketDir(f.Ticket) + "/"}
			o.Type = "board"
		}
	}
	return o
}

// Percentile of sorted durations.
func Percentile(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	i := int(p * float64(len(s)-1))
	return s[i]
}

// listsProject reports whether a note's frontmatter has a `projects:` list that
// holds the slug, among any number of entries.
func listsProject(note, slug string) bool {
	for _, line := range strings.Split(note, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "projects:")
		if !ok {
			continue
		}
		for _, e := range strings.Split(strings.Trim(strings.TrimSpace(rest), "[]"), ",") {
			if strings.Trim(strings.TrimSpace(e), `"'`) == slug {
				return true
			}
		}
	}
	return false
}

// isTicketNote reports whether rel is a ticket's own note.
func isTicketNote(rel string) bool {
	_, ok := note.RelNote(rel)
	return ok
}

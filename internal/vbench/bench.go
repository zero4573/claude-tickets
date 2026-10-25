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
	"github.com/zero4573/claude-tickets/internal/vsearch"
)

// Budget is the `--budget` the NEW strategy searches with.
const Budget = 2000

// ReadN is how many whole matching notes the OLD strategy reads.
const ReadN = 5

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

// Old is the grep-and-read strategy: kb = ls of the skill's folders, a
// case-insensitive grep -l for any keyword, then ReadN whole notes in path
// order; recall = the project's folder, its note, all decisions, the two
// latest logs and (for a ticket) the ticket's notes.
func Old(vault string, files []string, f Fact) Outcome {
	var ctx strings.Builder
	if f.Scenario == "kb" {
		ctx.WriteString(listing(vault, "projects", "knowledge-base", "references"))
		var hits []string
		for _, rel := range files {
			low := strings.ToLower(read(vault, rel))
			for _, k := range f.Keywords {
				if strings.Contains(low, k) {
					hits = append(hits, rel)
					break
				}
			}
		}
		ctx.WriteString(strings.Join(hits, "\n") + "\n")
		for i := 0; i < len(hits) && i < ReadN; i++ {
			ctx.WriteString(read(vault, hits[i]))
		}
	} else {
		pre := "projects/" + f.Project + "/"
		ctx.WriteString(listing(vault, "projects", "projects/"+f.Project))
		var logs []string
		for _, rel := range files {
			if !strings.HasPrefix(rel, pre) {
				continue
			}
			switch {
			case strings.Contains(rel, "-log-"):
				logs = append(logs, rel)
			case strings.Contains(rel, "-decision-") || rel == pre+f.Project+".md":
				ctx.WriteString(read(vault, rel))
			}
		}
		for i := max(0, len(logs)-2); i < len(logs); i++ {
			ctx.WriteString(read(vault, logs[i]))
		}
		if f.Ticket != "" {
			ctx.WriteString(listing(vault, "tickets/"+f.Ticket, "tickets/"+f.Ticket+"/tasks"))
			for _, rel := range files {
				if strings.HasPrefix(rel, "tickets/"+f.Ticket+"/") {
					ctx.WriteString(read(vault, rel))
				}
			}
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

// New is one `search --k 30 --budget 2000` call; recall questions filter by project
// (the ticket's folder for a ticket question).
func New(r *vsearch.Response, f Fact) Outcome {
	s := Render(r)
	return Outcome{Tokens: chunk.Tokens(s), Found: strings.Contains(s, f.Sentence)}
}

// SearchOptions are the vsearch options for a fact's question.
func SearchOptions(f Fact) vsearch.Options {
	o := vsearch.Options{K: 30, Budget: Budget}
	if f.Scenario == "recall" {
		o.Project = f.Project
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

// Package vbench generates a deterministic, realistically shaped vault and
// measures how many tokens an agent needs to find context in it the old way
// (ls, grep, whole-note reads) against the new way (ct vault search with a
// budget). It backs `make bench`.
package vbench

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Fact is one planted answer: Sentence sits alone in the section Section of
// the note Path, phrased differently from Question. Keywords are what an
// agent would grep for.
type Fact struct {
	Scenario string // "kb" or "recall"
	Question string
	Keywords []string
	Project  string // recall: the project slug
	Ticket   string // recall: the ticket id, when the question is about one
	Section  string
	Sentence string
	Path     string // vault-relative, set by Generate
}

// Vault is a generated vault.
type Vault struct {
	Dir   string
	Notes int
	Facts []Fact
}

var projectSlugs = strings.Fields(`acme borealis cobalt delta echo foxtrot golf hotel india juliet
kilo lima mike november oscar papa quebec romeo sierra tango uniform victor whiskey xray yankee zulu
amber basalt cedar dune ember fjord glacier harbor iris jade kestrel lumen meadow nimbus onyx`)

const nTickets = 60

var vocab = strings.Fields(`service component module handler pipeline scheduler registry connector
adapter parser renderer dispatcher monitor cache queue index router client server proxy broker
buffer stream channel driver loader mapper resolver builder planner runner watcher emitter filter
policy template profile account invoice report dashboard metric alert signal budget quota region
zone network subnet volume bucket folder archive snapshot release version branch commit review
deploy rollout canary staging pilot sandbox vendor contract license domain record entry field
column table view trigger script command option setting default fallback timeout window interval
batch shard replica leader follower quorum lease token header payload schema format encoder decoder`)

var adjectives = strings.Fields(`shared internal external legacy primary secondary optional stable
experimental reusable lightweight dedicated managed automated manual incremental central regional`)

var verbs = strings.Fields(`handles tracks wraps forwards validates converts records exposes
reads writes merges splits schedules prefers ignores reports measures`)

var tails = strings.Split("during normal operation|when the load is low|for every request|in most environments|unless configured otherwise|as described in the runbook|after the daily review|on each deployment", "|")

type gen struct {
	rng   *rand.Rand
	notes map[string]string
	names []string // note names, for wikilinks
	skip  map[string]bool
}

func (g *gen) word() string {
	for {
		w := vocab[g.rng.Intn(len(vocab))]
		if !g.skip[w] {
			return w
		}
	}
}

func (g *gen) sentence() string {
	a := adjectives[g.rng.Intn(len(adjectives))]
	v := verbs[g.rng.Intn(len(verbs))]
	t := tails[g.rng.Intn(len(tails))]
	s := fmt.Sprintf("The %s %s %s the %s %s %s.", a, g.word(), v, g.word(), g.word(), t)
	if g.rng.Intn(5) == 0 {
		s += fmt.Sprintf(" See [[%s]].", g.names[g.rng.Intn(len(g.names))])
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (g *gen) paragraph(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(g.sentence())
	}
	return b.String()
}

func (g *gen) list(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "- %s\n", g.sentence())
	}
	return b.String()
}

// section is a heading with a paragraph (and sometimes a list) of filler.
func (g *gen) section(level int, title string, size int) string {
	s := fmt.Sprintf("%s %s\n\n%s\n\n", strings.Repeat("#", level), title, g.paragraph(size))
	if g.rng.Intn(2) == 0 {
		s += g.list(2+g.rng.Intn(4)) + "\n"
	}
	return s
}

var topics = strings.Fields(`overview background scope goals constraints interfaces operations
monitoring security testing rollout dependencies history glossary examples limits`)

func (g *gen) body(sections, size int) string {
	var b strings.Builder
	for i := 0; i < sections; i++ {
		title := strings.ToUpper(topics[g.rng.Intn(len(topics))][:1]) + topics[g.rng.Intn(len(topics))][1:]
		b.WriteString(g.section(2, title, size))
	}
	return b.String()
}

func fm(kv ...string) string {
	var b strings.Builder
	b.WriteString("---\n")
	for i := 0; i+1 < len(kv); i += 2 {
		fmt.Fprintf(&b, "%s: %s\n", kv[i], kv[i+1])
	}
	b.WriteString("---\n")
	return b.String()
}

func (g *gen) put(path, name, front, title, body string) {
	g.notes[path] = front + "# " + title + "\n\n" + body
	g.names = append(g.names, name)
}

func recallFacts() []Fact {
	return []Fact{
		{Scenario: "recall", Project: "acme", Question: "What is blocking acme right now?", Keywords: []string{"acme", "blocking"},
			Section: "Open issues", Sentence: "Work is blocking on the vendor issuing sandbox credentials; integration tests are paused."},
		{Scenario: "recall", Project: "borealis", Question: "What did borealis decide about the storage engine?", Keywords: []string{"borealis", "decide", "storage"},
			Section: "Outcome", Sentence: "We chose the columnar store for storage after the spike showed 4x faster scans."},
		{Scenario: "recall", Project: "cobalt", Question: "What was done last session on cobalt?", Keywords: []string{"cobalt", "session"},
			Section: "Summary", Sentence: "Finished the session migration and merged the cache layer; the audit hook remains."},
		{Scenario: "recall", Project: "delta", Ticket: "PROJ-103", Question: "Which tasks are still open for the delta ticket?", Keywords: []string{"delta", "tasks", "open"},
			Section: "Status", Sentence: "Open: task 03 (parser) and task 05 (docs); task 04 is waiting in review."},
		{Scenario: "recall", Project: "echo", Question: "What did echo decide about authentication?", Keywords: []string{"echo", "decide", "authentication"},
			Section: "Outcome", Sentence: "Decided to move authentication to short-lived tokens and drop the legacy cookie path."},
		{Scenario: "recall", Project: "foxtrot", Question: "What are the next steps for foxtrot?", Keywords: []string{"foxtrot", "next", "steps"},
			Section: "Follow-ups", Sentence: "Next steps: cut the release candidate and rerun the soak test over the weekend."},
	}
}

func kbFacts() []Fact {
	f := func(q, kw, sec, s string) Fact {
		return Fact{Scenario: "kb", Question: q, Keywords: strings.Fields(kw), Section: sec, Sentence: s}
	}
	return []Fact{
		f("How long are audit exports kept before cleanup?", "audit exports cleanup", "Retention window", "Exports older than 93 days are purged nightly by the sweeper job."),
		f("Which port does the telemetry gateway listen on for ingest?", "telemetry gateway ingest port", "Listener", "The collector accepts batches on TCP 8443 behind the edge proxy, gateway side only."),
		f("What is the rollback procedure when a schema migration fails midway?", "rollback schema migration", "Recovery", "If a migration aborts, restore the pre-run snapshot named pre-migrate-DATE and rerun the failed step only."),
		f("Who approves emergency changes to the billing ledger?", "emergency billing ledger approve", "Approvals", "Out-of-hours edits to the ledger need sign-off from the finance on-call lead plus one platform engineer."),
		f("What timeout does the payment webhook use before retrying?", "payment webhook timeout retry", "Delivery", "A webhook delivery waits 12 seconds for an acknowledgement, then backs off exponentially up to six attempts."),
		f("How are feature flags named for the mobile client?", "feature flags mobile naming", "Naming scheme", "Feature toggles for the handset app follow the pattern m_AREA_NAME in lowercase."),
		f("What is the maximum upload size for customer attachments?", "upload size attachments customer", "Limits", "Customer files above 25 MB are rejected at the edge with a 413 response."),
		f("Which database holds session tokens and what is its eviction policy?", "session tokens database eviction", "Storage", "Session state lives in the redis cache cluster with allkeys-lru eviction and a 30 minute ttl."),
		f("How do we rotate the signing keys for the partner API?", "rotate signing keys partner", "Key rotation", "Signing keys for the partner interface rotate every quarter; the old key stays valid for 14 days."),
		f("What does the nightly reconciliation job compare?", "nightly reconciliation job compare", "Reconciliation", "The nightly run diffs ledger totals against the bank statements and files a ticket on any mismatch above 1 cent."),
		f("What is the on-call escalation order for the search cluster?", "escalation order search cluster oncall", "Escalation", "Page the primary first, then the secondary after 10 minutes, then the cluster owner."),
		f("Where are the load test results archived?", "load test results archived", "Artifacts", "Performance run outputs are uploaded to the perf-results bucket under the release tag."),
		f("What encryption is used for backups at rest?", "encryption backups rest", "Backups", "Snapshots are sealed with AES-256 using a per-environment key held in the vault service."),
		f("How many replicas does the notification worker run in production?", "replicas notification worker production", "Scaling", "The notifier deployment runs 6 pods in prod, scaling to 12 above 70 percent cpu."),
	}
}

// Generate writes a vault of at least 2,000 notes under dir. The same seed
// always yields the same bytes.
func Generate(dir string, seed int64) (*Vault, error) {
	g := &gen{rng: rand.New(rand.NewSource(seed)), notes: map[string]string{}, skip: map[string]bool{}}
	facts := append(kbFacts(), recallFacts()...)
	for _, f := range facts {
		for _, k := range f.Keywords {
			g.skip[k] = true
		}
	}
	// names first, so wikilinks can point at notes generated later
	for _, p := range projectSlugs {
		g.names = append(g.names, p, p+"-architecture")
	}
	for i := 0; i < nTickets; i++ {
		g.names = append(g.names, fmt.Sprintf("PROJ-%d", 100+i))
	}
	for i := 0; i < 300; i++ {
		g.names = append(g.names, fmt.Sprintf("kb-%03d", i))
	}
	base := len(g.names)
	_ = base

	logsOf := map[string][]string{}
	decisionsOf := map[string][]string{}
	for pi, p := range projectSlugs {
		other := projectSlugs[(pi+1)%len(projectSlugs)]
		g.put("projects/"+p+"/"+p+".md", p,
			fm("type", "project", "tags", "[project]", "groups", "[platform]", "depends-on", "["+other+"]"),
			strings.ToUpper(p[:1])+p[1:], g.body(4, 3))
		g.put("projects/"+p+"/"+p+"-architecture.md", p+"-architecture",
			fm("type", "architecture", "projects", "["+p+"]", "tags", "[architecture]"),
			p+" architecture", g.body(7, 4))
		for d := 1; d <= 6; d++ {
			path := fmt.Sprintf("projects/%s/%s-decision-%02d.md", p, p, d)
			g.put(path, fmt.Sprintf("%s-decision-%02d", p, d),
				fm("type", "decision", "projects", "["+p+"]", "tags", "[decision]"),
				fmt.Sprintf("Decision %02d", d), g.section(2, "Context", 3)+g.section(2, "Options", 2)+g.section(2, "Outcome", 2))
			decisionsOf[p] = append(decisionsOf[p], path)
		}
		for l := 0; l < 12; l++ {
			date := fmt.Sprintf("2026-%02d-%02d", 1+l/3, 3+(l%3)*9)
			path := fmt.Sprintf("projects/%s/%s-log-%s.md", p, p, date)
			g.put(path, fmt.Sprintf("%s-log-%s", p, date),
				fm("type", "log", "projects", "["+p+"]", "tags", "[log]"),
				"Session "+date, g.section(2, "Summary", 3)+g.section(2, "Open issues", 2)+g.section(2, "Follow-ups", 2))
			logsOf[p] = append(logsOf[p], path)
		}
	}
	for i := 0; i < nTickets; i++ {
		id := fmt.Sprintf("PROJ-%d", 100+i)
		p := projectSlugs[i%len(projectSlugs)]
		front := fm("type", "ticket", "ticket-type", "story", "status", "active", "projects", "["+p+"]")
		g.put(ticketDir(id)+"/"+id+".md", id, front, id, g.section(2, "Description", 3)+g.section(2, "Acceptance criteria", 2)+g.section(2, "Tasks", 2))
		g.put(ticketDir(id)+"/"+id+"-plan.md", id+"-plan", fm("type", "plan", "projects", "["+p+"]"), id+" plan", g.body(4, 3))
		g.put(ticketDir(id)+"/"+id+"-board.md", id+"-board", fm("type", "board", "projects", "["+p+"]"), id+" board", g.section(2, "Status", 2)+g.section(2, "Waves", 2))
		for t := 1; t <= 8; t++ {
			name := fmt.Sprintf("%s-%02d-task", id, t)
			g.put(ticketDir(id)+"/tasks/"+name+".md", name, fm("type", "task", "projects", "["+p+"]", "depends-on", "["+id+"]"), name, g.body(3, 3))
		}
	}
	for i := 0; i < 300; i++ {
		g.put(fmt.Sprintf("knowledge-base/kb-%03d.md", i), fmt.Sprintf("kb-%03d", i), fm("type", "knowledge", "tags", "[kb]"),
			fmt.Sprintf("Knowledge %03d", i), g.body(3+g.rng.Intn(4), 3))
	}
	for i := 0; i < 250; i++ {
		g.put(fmt.Sprintf("references/ref-%03d.md", i), fmt.Sprintf("ref-%03d", i), fm("type", "reference", "tags", "[reference]"),
			fmt.Sprintf("Reference %03d", i), g.body(2+g.rng.Intn(3), 3))
	}
	for i := 0; i < 150; i++ {
		g.put(fmt.Sprintf("learning/learn-%03d.md", i), fmt.Sprintf("learn-%03d", i), fm("type", "learning", "tags", "[learning]"),
			fmt.Sprintf("Learning %03d", i), g.body(2+g.rng.Intn(3), 3))
	}

	// plant the facts
	paths := sortedPaths(g.notes)
	var kbPaths []string
	for _, p := range paths {
		if strings.HasPrefix(p, "knowledge-base/") || strings.HasSuffix(p, "-architecture.md") {
			kbPaths = append(kbPaths, p)
		}
	}
	kbIdx := 0
	for i := range facts {
		f := &facts[i]
		sec := fmt.Sprintf("## %s\n\n%s\n", f.Section, f.Sentence)
		switch {
		case f.Scenario == "kb":
			f.Path = kbPaths[(kbIdx*37+11)%len(kbPaths)]
			kbIdx++
		case f.Ticket != "":
			f.Path = ticketDir(f.Ticket) + "/" + f.Ticket + "-board.md"
		case strings.Contains(f.Sentence, "Decided") || strings.Contains(f.Sentence, "chose"):
			f.Path = decisionsOf[f.Project][2]
		default:
			ls := logsOf[f.Project]
			f.Path = ls[len(ls)-1] // the latest log
		}
		g.notes[f.Path] += "\n" + sec
		// distractors: other notes that share the keywords, with different values
		for d := 0; d < 12; d++ {
			q := paths[g.rng.Intn(len(paths))]
			if q == f.Path || strings.HasPrefix(q, "projects/"+f.Project+"/") && f.Scenario == "recall" {
				continue
			}
			kws := strings.Join(f.Keywords, " and ")
			g.notes[q] += fmt.Sprintf("\n## Notes on %s\n\nThe %s topics are handled by another team; values here differ (%d). %s\n",
				f.Keywords[0], kws, 2+g.rng.Intn(90), g.sentence())
		}
	}
	for p, body := range g.notes {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			return nil, err
		}
	}
	return &Vault{Dir: dir, Notes: len(g.notes), Facts: facts}, nil
}

func sortedPaths(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for p := range m {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// ticketDir is a generated ticket's folder: they all come from Jira.
func ticketDir(id string) string { return "tickets/jira/" + id }

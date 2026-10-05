package main_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/zero4573/claude-tickets/internal/mcp/mcptest"
)

var (
	fakeMu    sync.Mutex
	fakeJiras = map[string]*mcptest.Server{}
)

// fakejira <fixture>: serves the fixture (testdata/sync/*.json) as the
// Atlassian MCP server named atlassian in $WORK/mcp.json (set as
// CLAUDE_TICKETS_MCP_CONFIG); later calls switch the fixture.
func fakejira(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) != 1 {
		ts.Fatalf("usage: fakejira <fixture>")
	}
	f, err := mcptest.LoadFixture(ts.MkAbs(args[0]))
	ts.Check(err)
	work := ts.Getenv("WORK")
	fakeMu.Lock()
	defer fakeMu.Unlock()
	if s, ok := fakeJiras[work]; ok {
		s.SetFixture(f)
		return
	}
	s := mcptest.New(f)
	fakeJiras[work] = s
	ts.Defer(func() {
		fakeMu.Lock()
		delete(fakeJiras, work)
		fakeMu.Unlock()
		s.Close()
	})
	cfg := filepath.Join(work, "mcp.json")
	ts.Check(os.WriteFile(cfg, []byte(s.Config("atlassian")), 0o644))
	ts.Setenv("CLAUDE_TICKETS_MCP_CONFIG", cfg)
}

var (
	clockRe = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})-\d{4}-`)
	// <date>-<HHMM>-<cmd>-follow-ups[-<n>].md
	followupRe = regexp.MustCompile(`^inbox/(\d{4}-\d{2}-\d{2}-\d{4})-(.+)-follow-ups(?:-(\d+))?\.md$`)
	stampRe    = regexp.MustCompile(`\(\d{4}-\d{2}-\d{2} \d{2}:\d{2}\)`)
)

// normalizedVault is a vault's tickets/ and inbox/ files, with what changes
// from run to run masked: today's date, lastSync, follow-up note times.
// Follow-up notes are numbered in the order they were written
// (<cmd>-follow-ups-<k>.md): whether two runs share a minute (and so a
// -2 suffix) depends on timing.
func normalizedVault(vault string) (map[string]string, error) {
	today := time.Now().Format("2006-01-02")
	out := map[string]string{}
	type followupNote struct {
		stamp, cmd, name, body string
		n                      int
	}
	var followups []followupNote
	err := filepath.Walk(vault, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(vault, p)
		if info.IsDir() {
			if top := strings.Split(filepath.ToSlash(rel), "/")[0]; rel != "." && top != "tickets" && top != "inbox" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.Contains(filepath.ToSlash(rel), "/") {
			return nil // files at the vault's top
		}
		if strings.HasSuffix(rel, ".lock") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		s := string(data)
		if m := followupRe.FindStringSubmatch(filepath.ToSlash(rel)); m != nil {
			n := 1
			if m[3] != "" {
				n, _ = strconv.Atoi(m[3])
			}
			name := strings.TrimSuffix(filepath.Base(rel), ".md")
			followups = append(followups, followupNote{m[1], m[2], name, s, n})
			return nil
		}
		if rel == filepath.Join("tickets", ".sync-state.json") {
			var doc map[string]any
			if json.Unmarshal(data, &doc) == nil {
				if src, ok := doc["sources"].(map[string]any); ok {
					for _, v := range src {
						if m, ok := v.(map[string]any); ok {
							delete(m, "lastSync")
						}
					}
				}
				b, _ := json.MarshalIndent(doc, "", "  ")
				s = string(b) + "\n"
			}
		}
		rel = clockRe.ReplaceAllString(filepath.ToSlash(rel), "$1-HHMM-")
		s = clockRe.ReplaceAllString(s, "$1-HHMM-")
		s = stampRe.ReplaceAllString(s, "(TIME)")
		s = strings.ReplaceAll(s, today, "TODAY")
		rel = strings.ReplaceAll(rel, today, "TODAY")
		out[rel] = s
		return nil
	})
	sort.Slice(followups, func(i, j int) bool {
		if followups[i].stamp != followups[j].stamp {
			return followups[i].stamp < followups[j].stamp
		}
		return followups[i].n < followups[j].n
	})
	for k, f := range followups {
		name := fmt.Sprintf("%s-follow-ups-%d", f.cmd, k+1)
		s := strings.Replace(f.body, "title: "+f.name+"\n", "title: "+name+"\n", 1)
		s = stampRe.ReplaceAllString(s, "(TIME)")
		out["inbox/"+name+".md"] = strings.ReplaceAll(s, today, "TODAY")
	}
	return out, err
}

// cmpvault <vault> <golden dir>: the vault matches the golden files
// (normalizedVault); UPDATE_GOLDEN=1 in the environment rewrites them.
func cmpvault(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) != 2 {
		ts.Fatalf("usage: cmpvault <vault> <golden dir>")
	}
	got, err := normalizedVault(ts.MkAbs(args[0]))
	ts.Check(err)
	golden := ts.MkAbs(args[1])
	if os.Getenv("UPDATE_GOLDEN") != "" {
		ts.Check(os.RemoveAll(golden))
		for rel, s := range got {
			p := filepath.Join(golden, rel)
			ts.Check(os.MkdirAll(filepath.Dir(p), 0o755))
			ts.Check(os.WriteFile(p, []byte(s), 0o644))
		}
		return
	}
	want, err := normalizedVault(golden)
	ts.Check(err)
	var problems []string
	for rel, w := range want {
		g, ok := got[rel]
		switch {
		case !ok:
			problems = append(problems, "missing: "+rel)
		case g != w:
			problems = append(problems, fmt.Sprintf("differs: %s\n--- want\n%s\n--- got\n%s", rel, w, g))
		}
	}
	for rel := range got {
		if _, ok := want[rel]; !ok {
			problems = append(problems, "unexpected: "+rel)
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		ts.Fatalf("%s doesn't match %s (UPDATE_GOLDEN=1 to accept):\n%s", args[0], args[1], strings.Join(problems, "\n"))
	}
}

package main_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/zero4573/claude-tickets/internal/mcp/mcptest"
)

// The fake Jira of each script (fakejira), by work dir.
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
	stampRe = regexp.MustCompile(`\(\d{4}-\d{2}-\d{2} \d{2}:\d{2}\)`)
)

// normalizedVault is a vault's tickets/ and inbox/ files, with what changes
// from run to run masked: today's date, lastSync, follow-up note times.
func normalizedVault(vault string) (map[string]string, error) {
	today := time.Now().Format("2006-01-02")
	out := map[string]string{}
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

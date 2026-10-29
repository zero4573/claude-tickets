package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const relaySeed = "# Relay\n\n## Closed\n<!-- c -->\nR-84 → T-01 merged\n\n## Open\n<!-- o -->\nR-85 · 2026-01-01 · user→lead · request · first · open\n"

func relayFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "relay.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

var relayNow = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func TestRelayAddNextID(t *testing.T) {
	p := relayFile(t, relaySeed)
	id, err := relayAdd(p, "po", "lead", "question", "open", "why?", relayNow, time.Second)
	if err != nil || id != "R-86" {
		t.Fatalf("id %q err %v", id, err)
	}
	b, _ := os.ReadFile(p)
	if !strings.HasSuffix(string(b), "R-86 · 2026-10-29 · po→lead · question · why? · open\n") {
		t.Fatalf("file:\n%s", b)
	}
}

func TestRelayAddBeforeNextSection(t *testing.T) {
	p := relayFile(t, "## Open\nR-81 · d · a→b · request · x · open\n\n## Other\nz\n")
	if _, err := relayAdd(p, "user", "po", "answer", "planned", "t", relayNow, time.Second); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	want := "## Open\nR-81 · d · a→b · request · x · open\nR-82 · 2026-10-29 · user→po · answer · t · planned\n\n## Other\nz\n"
	if string(b) != want {
		t.Fatalf("got:\n%s", b)
	}
}

func TestRelayAddConcurrent(t *testing.T) {
	p := relayFile(t, relaySeed)
	const n = 20
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := relayAdd(p, "po", "lead", "feedback", "open", "msg "+string(rune('a'+i)), relayNow, 20*time.Second)
			if err != nil {
				t.Error(err)
			}
			ids[i] = id
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate id %s in %v", id, ids)
		}
		seen[id] = true
	}
	b, _ := os.ReadFile(p)
	if got := len(regexp.MustCompile(`(?m)^R-\d+ · 2026-10-29`).FindAll(b, -1)); got != n {
		t.Fatalf("%d lines, want %d:\n%s", got, n, b)
	}
}

func TestRelayAddValidation(t *testing.T) {
	p := relayFile(t, relaySeed)
	long := strings.Repeat("x", 201)
	for name, c := range map[string][5]string{
		"from":    {"bob", "po", "request", "open", "t"},
		"to":      {"po", "bob", "request", "open", "t"},
		"kind":    {"po", "lead", "chat", "open", "t"},
		"state":   {"po", "lead", "request", "done", "t"},
		"empty":   {"po", "lead", "request", "open", " "},
		"newline": {"po", "lead", "request", "open", "a\nb"},
		"sep":     {"po", "lead", "request", "open", "a · b"},
		"long":    {"po", "lead", "request", "open", long},
	} {
		if _, err := relayAdd(p, c[0], c[1], c[2], c[3], c[4], relayNow, time.Second); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := relayAdd(p, "po", "lead", "request", "open", strings.Repeat("é", 200), relayNow, time.Second); err != nil {
		t.Errorf("200 runes refused: %v", err)
	}
	if b, _ := os.ReadFile(p); strings.Count(string(b), "\n") != strings.Count(relaySeed, "\n")+1 {
		t.Errorf("rejected adds changed the file:\n%s", b)
	}
}

func TestRelayLockTimeout(t *testing.T) {
	p := relayFile(t, relaySeed)
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = relayWriteLocked(p, time.Second, func(s string) (string, error) {
			close(held)
			<-release
			return s, nil
		})
	}()
	<-held
	_, err := relayAdd(p, "po", "lead", "request", "open", "t", relayNow, 300*time.Millisecond)
	close(release)
	<-done
	if err == nil || !strings.Contains(err.Error(), "held by another process") {
		t.Fatalf("err = %v", err)
	}
}

func TestRelaySetState(t *testing.T) {
	p := relayFile(t, relaySeed)
	if err := relaySetState(p, "R-85", "copied", time.Second); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "request · first · copied\n") || strings.Contains(string(b), "· open\n") {
		t.Fatalf("file:\n%s", b)
	}
	if err := relaySetState(p, "R-99", "open", time.Second); err == nil {
		t.Error("missing id: no error")
	}
	if err := relaySetState(p, "R-85", "bogus", time.Second); err == nil {
		t.Error("bad state: no error")
	}
	if err := relaySetState(p, "five", "open", time.Second); err == nil {
		t.Error("bad id: no error")
	}
}

func TestRelayIDAfterClosingHighest(t *testing.T) {
	p := relayFile(t, "# Relay\n\n## Closed\nR-67 → T-03 merged\n\n## Open\n")
	id, err := relayAdd(p, "po", "lead", "request", "open", "x", relayNow, time.Second)
	if err != nil || id != "R-68" {
		t.Fatalf("id %q err %v", id, err)
	}
	if err := relayClose(p, "R-68", "done", time.Second); err != nil {
		t.Fatal(err)
	}
	id, err = relayAdd(p, "po", "lead", "request", "open", "y", relayNow, time.Second)
	if err != nil || id != "R-69" {
		t.Fatalf("after closing the highest: id %q err %v", id, err)
	}
	b, _ := os.ReadFile(p)
	want := "# Relay\n\n## Closed\nR-67 → T-03 merged\nR-68 → done\n\n## Open\nR-69 · 2026-10-29 · po→lead · request · y · open\n"
	if string(b) != want {
		t.Fatalf("got:\n%s", b)
	}
}

func TestRelayCloseKeepsForty(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("## Closed\n<!-- c -->\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&sb, "R-%d → s\n", 50+i)
	}
	sb.WriteString("\n## Open\nR-91 · d · a→b · request · x · open\n")
	p := relayFile(t, sb.String())
	if err := relayClose(p, "R-91", "last", time.Second); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	got := string(b)
	if n := strings.Count(got, " → "); n != 40 {
		t.Fatalf("%d closed lines:\n%s", n, got)
	}
	if strings.Contains(got, "R-51 → s") || !strings.Contains(got, "R-52 → s\n") || !strings.Contains(got, "R-91 → last\n") || !strings.Contains(got, "<!-- c -->") {
		t.Fatalf("wrong lines dropped:\n%s", got)
	}
	if strings.Contains(got, "R-91 ·") {
		t.Fatalf("entry still open:\n%s", got)
	}
	if err := relayClose(p, "R-91", "again", time.Second); err == nil {
		t.Error("closing a closed entry: no error")
	}
	if err := relayClose(p, "R-1234567", "x", time.Second); err == nil {
		t.Error("7 digits: no error")
	}
}

func TestRelayCloseConcurrentWithAdd(t *testing.T) {
	p := relayFile(t, "## Closed\n\n## Open\nR-61 · d · a→b · request · x · open\n")
	var wg sync.WaitGroup
	const n = 10
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := relayAdd(p, "po", "lead", "request", "open", "m", relayNow, 20*time.Second)
			if err != nil {
				t.Error(err)
			}
			ids[i] = id
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := relayClose(p, "R-61", "done", 20*time.Second); err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()
	seen := map[string]bool{"R-61": true}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate or reused id %s in %v", id, ids)
		}
		seen[id] = true
	}
	b, _ := os.ReadFile(p)
	if got := len(regexp.MustCompile(`(?m)^R-\d+ · `).FindAll(b, -1)); got != n {
		t.Fatalf("%d open lines, want %d:\n%s", got, n, b)
	}
	if !strings.Contains(string(b), "R-61 → done\n") {
		t.Fatalf("not closed:\n%s", b)
	}
}

func TestRelayTextAndIDCaps(t *testing.T) {
	p := relayFile(t, relaySeed)
	for name, text := range map[string]string{
		"nul": "a\x00b", "tab": "a\tb", "ls": "a\u2028b", "ps": "a\u2029b", "nel": "a\u0085b", "esc": "a\x1bb", "cr": "a\rb",
	} {
		if _, err := relayAdd(p, "po", "lead", "request", "open", text, relayNow, time.Second); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	huge := relayFile(t, "## Open\nR-99999999999999999999 · d · a→b · request · x · open\n")
	if _, err := relayAdd(huge, "po", "lead", "request", "open", "t", relayNow, time.Second); err == nil {
		t.Error("huge id: no error")
	}
	full := relayFile(t, "## Open\nR-999999 · d · a→b · request · x · open\n")
	if _, err := relayAdd(full, "po", "lead", "request", "open", "t", relayNow, time.Second); err == nil {
		t.Error("id past 999999: no error")
	}
	if err := relaySetState(p, "R-1234567", "open", time.Second); err == nil {
		t.Error("7-digit id: no error")
	}
}

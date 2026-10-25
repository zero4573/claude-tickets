package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zero4573/claude-tickets/internal/ticketmigrate"
	"github.com/zero4573/claude-tickets/internal/vaultlock"
)

func updateVault(t *testing.T) string {
	t.Helper()
	v := t.TempDir()
	if err := os.Mkdir(filepath.Join(v, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	return v
}

func stubMigrate(t *testing.T, fn func(vault string, tpl []byte, today string, dry bool) (ticketmigrate.Result, error)) {
	t.Helper()
	orig := migrateTickets
	t.Cleanup(func() { migrateTickets = orig })
	migrateTickets = fn
}

// The vault lock is held through Apply and the migration, and released
// after, also when the migration fails; a dry run never takes it.
func TestVaultUpdateLock(t *testing.T) {
	v := updateVault(t)
	dir, _ := vaultlock.Dir(v)
	var heldDuring, dryHeld string
	var tplOK bool
	stubMigrate(t, func(vault string, tpl []byte, today string, dry bool) (ticketmigrate.Result, error) {
		if dry {
			dryHeld = vaultlock.Holder(dir)
		} else {
			heldDuring = vaultlock.Holder(dir)
		}
		tplOK = strings.Contains(string(tpl), "TICKET-ID status")
		return ticketmigrate.Result{}, nil
	})
	if err := vaultUpdate(v, updateOpts{dryRun: true}); err != nil {
		t.Fatal(err)
	}
	if dryHeld != "" {
		t.Errorf("a dry run held the lock (%q)", dryHeld)
	}
	if err := vaultUpdate(v, updateOpts{}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(heldDuring, "ct-vault-update-") {
		t.Errorf("lock holder during the migration: %q", heldDuring)
	}
	if !tplOK {
		t.Error("the migration wasn't given the status template")
	}
	if h := vaultlock.Holder(dir); h != "" {
		t.Errorf("lock still held by %q", h)
	}

	// released on an error
	stubMigrate(t, func(string, []byte, string, bool) (ticketmigrate.Result, error) {
		return ticketmigrate.Result{}, errors.New("boom")
	})
	if err := vaultUpdate(v, updateOpts{}); err == nil || err.Error() != "boom" {
		t.Fatalf("err %v", err)
	}
	if h := vaultlock.Holder(dir); h != "" {
		t.Errorf("lock held after an error: %q", h)
	}
}

// What a run wrote or removed is refreshed in the index, once; nothing
// written, no call; a dry run, no call.
func TestVaultUpdateRefreshesWrittenPaths(t *testing.T) {
	v := updateVault(t)
	var calls [][]string
	orig := refreshIndex
	t.Cleanup(func() { refreshIndex = orig })
	refreshIndex = func(_ string, files ...string) { calls = append(calls, files) }
	stubMigrate(t, func(string, []byte, string, bool) (ticketmigrate.Result, error) {
		return ticketmigrate.Result{Migrated: 1, Tickets: []ticketmigrate.TicketChange{{
			ID: "A-1", StatusAdded: true, StatusRel: "tickets/A-1/A-1-status.md",
			Files: []ticketmigrate.FileChange{{Rel: "tickets/A-1/board.md"}},
		}}}, nil
	})
	// an unedited old view to be removed
	if err := os.WriteFile(filepath.Join(v, "pending.md"), []byte("```tasks\nnot done\npath does not include templates/\nsort by created reverse\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := vaultUpdate(v, updateOpts{dryRun: true}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("a dry run refreshed: %v", calls)
	}
	if err := vaultUpdate(v, updateOpts{}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls %v", calls)
	}
	have := map[string]bool{}
	for _, p := range calls[0] {
		have[p] = true
	}
	for _, want := range []string{"inbox.md", "templates/status.md", "pending.md", "tickets/A-1/A-1-status.md", "tickets/A-1/board.md"} {
		if !have[want] {
			t.Errorf("%s not refreshed: %v", want, calls[0])
		}
	}
	// nothing more to write: no call
	calls = nil
	stubMigrate(t, func(string, []byte, string, bool) (ticketmigrate.Result, error) { return ticketmigrate.Result{}, nil })
	if err := vaultUpdate(v, updateOpts{}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Errorf("a no-op run refreshed: %v", calls)
	}
}

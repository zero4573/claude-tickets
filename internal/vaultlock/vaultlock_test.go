package vaultlock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLock(t *testing.T) {
	v := t.TempDir()
	if _, err := Status(v); err == nil {
		t.Fatal("not a vault: want an error")
	}
	os.Mkdir(filepath.Join(v, ".obsidian"), 0o755)
	Wait, poll = 50*time.Millisecond, 10*time.Millisecond
	if s, _ := Status(v); s != "unlocked" {
		t.Fatal(s)
	}
	if err := Acquire(v, "PROJ-1"); err != nil {
		t.Fatal(err)
	}
	if err := Acquire(v, "PROJ-1"); err != nil {
		t.Fatal("re-acquire as the same owner:", err)
	}
	if s, _ := Status(v); s != "locked by 'PROJ-1'" {
		t.Fatal(s)
	}
	if err := Acquire(v, "PROJ-2"); err == nil || !strings.Contains(err.Error(), "still locked by 'PROJ-1'") {
		t.Fatal("held by another: ", err)
	}
	if err := Release(v, "PROJ-2"); err == nil {
		t.Fatal("release by another owner: want an error")
	}
	// Abandoned: taken over
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(v, ".vault.lock.d", "owner"), old, old)
	if err := Acquire(v, "PROJ-2"); err != nil {
		t.Fatal("take over:", err)
	}
	if err := Release(v, "PROJ-2"); err != nil {
		t.Fatal(err)
	}
	if err := Release(v, "PROJ-2"); err != nil {
		t.Fatal("release when free:", err)
	}
}

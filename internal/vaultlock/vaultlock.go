// Package vaultlock is the exclusive lock on a vault's shared notes
// (projects/, knowledge-base/, references/), so concurrent sessions running
// /tickets:save take turns. The lock is the directory <vault>/.vault.lock.d
// (mkdir is atomic) holding an owner file; stale takeovers serialize on
// <vault>/.vault.lock.d.guard. Dot-files are hidden from Obsidian.
package vaultlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zero4573/claude-tickets/internal/lock"
)

var (
	// Wait is how long Acquire waits for the lock.
	Wait = 10 * time.Minute
	// Stale is the age (of the owner file) past which a lock is abandoned.
	Stale = 30 * time.Minute
	poll  = 2 * time.Second
)

// Dir is a vault's lock directory.
func Dir(vault string) (string, error) {
	if st, err := os.Stat(filepath.Join(vault, ".obsidian")); err != nil || !st.IsDir() {
		return "", fmt.Errorf("not an Obsidian vault: %s", vault)
	}
	return filepath.Join(vault, ".vault.lock.d"), nil
}

// Holder is who holds the lock ("" if nobody).
func Holder(dir string) string {
	data, _ := os.ReadFile(filepath.Join(dir, "owner"))
	return strings.TrimSpace(string(data))
}

func age(dir string) time.Duration {
	st, err := os.Stat(filepath.Join(dir, "owner"))
	if err != nil {
		return 0
	}
	return time.Since(st.ModTime())
}

func take(dir, owner string) bool {
	if os.Mkdir(dir, 0o755) != nil {
		return false
	}
	_ = os.WriteFile(filepath.Join(dir, "owner"), []byte(owner+"\n"), 0o644)
	return true
}

// takeOver replaces an abandoned lock with ours. Two waiters can find it
// stale at once, so the check is repeated under a guard lock: the second
// then sees the first one's fresh lock and keeps waiting.
func takeOver(dir, owner string) bool {
	ok := false
	_ = lock.With(dir+".guard", func() error {
		if a := age(dir); a > Stale {
			fmt.Fprintf(os.Stderr, "ct: taking over a %ds-old lock held by '%s'\n", int(a.Seconds()), Holder(dir))
			_ = os.RemoveAll(dir)
			ok = take(dir, owner)
		}
		return nil
	})
	return ok
}

// Acquire waits (up to Wait) until the lock is free, then takes it for
// owner. Re-acquiring as the same owner succeeds (and refreshes it).
func Acquire(vault, owner string) error {
	dir, err := Dir(vault)
	if err != nil {
		return err
	}
	if owner == "" {
		return errors.New("acquire: <owner> is required")
	}
	start := time.Now()
	for {
		if take(dir, owner) {
			return nil
		}
		if Holder(dir) == owner {
			now := time.Now()
			return os.Chtimes(filepath.Join(dir, "owner"), now, now)
		}
		if age(dir) > Stale && takeOver(dir, owner) {
			return nil
		}
		waited := time.Since(start)
		if waited >= Wait {
			return fmt.Errorf("acquire: still locked by '%s' after %ds", Holder(dir), int(Wait.Seconds()))
		}
		if waited < poll {
			fmt.Fprintf(os.Stderr, "ct: waiting for the vault lock held by '%s'...\n", Holder(dir))
		}
		time.Sleep(poll)
	}
}

// Release frees the lock if owner holds it.
func Release(vault, owner string) error {
	dir, err := Dir(vault)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	if h := Holder(dir); h != owner {
		return fmt.Errorf("release: the lock is held by '%s', not '%s'", h, owner)
	}
	return os.RemoveAll(dir)
}

// Status is "locked by '<owner>'" or "unlocked".
func Status(vault string) (string, error) {
	dir, err := Dir(vault)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(dir); err != nil {
		return "unlocked", nil
	}
	return fmt.Sprintf("locked by '%s'", Holder(dir)), nil
}

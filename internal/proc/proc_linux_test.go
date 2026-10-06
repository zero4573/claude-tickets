//go:build linux

package proc

import (
	"errors"
	"testing"
)

func TestParseStat(t *testing.T) {
	// A command name with spaces and a paren: fields count after the last ')'
	stat := "4242 (my (odd) cmd) S 1 4242 4242 0 -1 4194560 100 0 0 0 1 2 0 0 20 0 1 0 987654 1000 10\n"
	got, err := parseStat(stat, "boot")
	if err != nil || got != "linux:boot:987654" {
		t.Errorf("parseStat = %q, %v", got, err)
	}
	zombie := "4242 (sh) Z 1 4242 4242 0 -1 4194560 100 0 0 0 1 2 0 0 20 0 1 0 987654 1000 10\n"
	if _, err := parseStat(zombie, "boot"); !errors.Is(err, ErrNotRunning) {
		t.Errorf("zombie: err = %v, want ErrNotRunning", err)
	}
	if _, err := parseStat("garbage", "boot"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("garbage: err = %v, want ErrUnsupported", err)
	}
}

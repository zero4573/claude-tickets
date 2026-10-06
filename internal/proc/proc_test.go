package proc

import (
	"errors"
	"os"
	"os/exec"
	"testing"
)

func TestIdentityOwnProcess(t *testing.T) {
	a, err := Identity(os.Getpid())
	if errors.Is(err, ErrUnsupported) {
		t.Skip("no process identity on this OS")
	}
	if err != nil || a == "" {
		t.Fatalf("Identity(self) = %q, %v", a, err)
	}
	if b, err := Identity(os.Getpid()); err != nil || b != a {
		t.Errorf("not stable: %q then %q (%v)", a, b, err)
	}
}

func TestIdentityEndedProcess(t *testing.T) {
	if _, err := Identity(os.Getpid()); errors.Is(err, ErrUnsupported) {
		t.Skip("no process identity on this OS")
	}
	// The test binary itself, running no tests: it starts and exits at once
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := Identity(cmd.Process.Pid); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Identity(ended child) err = %v, want ErrNotRunning", err)
	}
}

func TestIdentityNoProcess(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if _, err := Identity(pid); !errors.Is(err, ErrNotRunning) {
			t.Errorf("Identity(%d) err = %v, want ErrNotRunning", pid, err)
		}
	}
	if _, err := Identity(os.Getpid()); errors.Is(err, ErrUnsupported) {
		t.Skip("no process identity on this OS")
	}
	// Above every OS's PID limit (Linux's is 2^22)
	if _, err := Identity(1 << 30); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Identity(huge) err = %v, want ErrNotRunning", err)
	}
}

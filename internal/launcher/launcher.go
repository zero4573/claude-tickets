// Package launcher runs ticket sessions. A backend with windows (tmux) runs
// each session in a window the user can come back to: one session group
// per vault (tickets-<vault>), one window per ticket, named by its ID.
// Without one (the none backend), a session runs in the foreground, in the
// terminal ct was started from (Run). Which backend: CLAUDE_TICKETS_LAUNCHER,
// else config.json's launcher, else the first one installed (Resolve). A
// new backend (e.g. zellij, Windows Terminal) implements Launcher, declares
// what it can do in Caps, and gets one entry in registry.go.
package launcher

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"strings"
)

// Window is a session to open.
type Window struct {
	Group, Name string // e.g. tickets-acme, PROJ-12
	Dir         string
	Argv        []string
	// Env is set for the session (also on a long-running backend server
	// that wouldn't pass the caller's environment on)
	Env map[string]string
	// Unset is kept out of a backend server started now (variables shells
	// opened there later shouldn't inherit)
	Unset []string
}

// Caps is what a backend can do. Callers branch on these, never on Name.
type Caps struct {
	Background bool // Open starts a session that outlives ct, so several can run at once
	List       bool // Windows lists a group's open windows
	SendKeys   bool // SendKeys types into an open window
	Attach     bool // Attach brings a window to the front
}

type Launcher interface {
	Name() string
	Caps() Caps
	// Windows is nil when the group doesn't exist (always, without List).
	Windows(group string) []string
	// Open starts w in a new background window, kept open after the
	// session ends so its output stays readable (ErrUnsupported without
	// Background).
	Open(w Window) error
	// SendKeys presses Enter after text (ErrUnsupported without SendKeys).
	SendKeys(group, name, text string) error
	// Attach brings a window to the front (ErrUnsupported without Attach).
	Attach(group, name string) error
}

func HasWindow(l Launcher, group, name string) bool {
	for _, w := range l.Windows(group) {
		if w == name {
			return true
		}
	}
	return false
}

var ErrUnsupported = errors.New("not supported without a terminal multiplexer")

// none has no windows: sessions run in the foreground (Run).
type none struct{}

func (none) Name() string                          { return "none" }
func (none) Caps() Caps                            { return Caps{} }
func (none) Windows(string) []string               { return nil }
func (none) Open(Window) error                     { return ErrUnsupported }
func (none) SendKeys(string, string, string) error { return ErrUnsupported }
func (none) Attach(string, string) error           { return ErrUnsupported }

// Run runs w in this terminal, in the foreground: cwd w.Dir, the
// environment with w.Env set (Unset doesn't apply: there's no server).
// Where the OS can exec, ct is replaced by w's program and Run returns only
// on failure. Elsewhere (Windows) the program runs as a child with ct's
// stdio, ct ignores Ctrl-C until it exits (the console sends it to both, so
// the program handles it and ct isn't killed first), and its
// *exec.ExitError is returned.
func Run(w Window) error {
	if len(w.Argv) == 0 {
		return errors.New("no command to run")
	}
	path, err := exec.LookPath(w.Argv[0])
	if err != nil {
		return err
	}
	if err := os.Chdir(w.Dir); err != nil {
		return err
	}
	env := mergeEnv(os.Environ(), w.Env)
	if execReplace(path, w.Argv, env) == nil {
		return nil
	}
	cmd := exec.Command(path, w.Argv[1:]...)
	cmd.Dir, cmd.Env = w.Dir, env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	signal.Ignore(os.Interrupt)
	defer signal.Reset(os.Interrupt)
	return cmd.Run()
}

// mergeEnv is environ with set's variables replacing (not duplicating)
// those already there.
func mergeEnv(environ []string, set map[string]string) []string {
	out := make([]string, 0, len(environ)+len(set))
	for _, kv := range environ {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := set[k]; !ok {
			out = append(out, kv)
		}
	}
	for k, v := range set {
		out = append(out, k+"="+v)
	}
	return out
}

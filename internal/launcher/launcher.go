// Package launcher runs ticket sessions in windows the user can come back
// to: one session group per vault (tickets-<vault>), one window per
// ticket, named by its ID. tmux is the only backend today; a machine
// without it (native Windows) gets the none backend, which answers queries
// with nothing and refuses to open or attach. Another backend (e.g.
// Windows Terminal) implements Launcher and is picked in Get.
package launcher

import (
	"errors"
	"os/exec"
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

type Launcher interface {
	Name() string
	// Windows is nil when the group doesn't exist.
	Windows(group string) []string
	// Open starts w in a new background window, kept open after the
	// session ends so its output stays readable.
	Open(w Window) error
	// SendKeys presses Enter after text.
	SendKeys(group, name, text string) error
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

func Get() Launcher {
	if _, err := exec.LookPath("tmux"); err == nil {
		return Tmux{}
	}
	return none{}
}

var ErrNoBackend = errors.New("ticket sessions need tmux (on Windows, run ct under WSL)")

type none struct{}

func (none) Name() string                          { return "none" }
func (none) Windows(string) []string               { return nil }
func (none) Open(Window) error                     { return ErrNoBackend }
func (none) SendKeys(string, string, string) error { return ErrNoBackend }
func (none) Attach(string, string) error           { return ErrNoBackend }

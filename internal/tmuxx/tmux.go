// Package tmuxx talks to tmux: the ticket windows live in one session per
// vault (tickets-<vault>), one window per ticket, named by its ID.
package tmuxx

import (
	"os"
	"os/exec"
	"strings"
)

// Windows lists the window names of session (none if it doesn't exist).
func Windows(session string) []string {
	out, err := exec.Command("tmux", "list-windows", "-t", "="+session, "-F", "#W").Output()
	if err != nil {
		return nil
	}
	var names []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			names = append(names, l)
		}
	}
	return names
}

// HasWindow reports whether session has a window named name.
func HasWindow(session, name string) bool {
	for _, w := range Windows(session) {
		if w == name {
			return true
		}
	}
	return false
}

// Attach selects window name of session and shows it: switches the client
// when already inside tmux, else attaches this terminal (replacing the
// process, so it returns only on failure).
func Attach(session, name string) error {
	if err := exec.Command("tmux", "select-window", "-t", "="+session+":"+name).Run(); err != nil {
		return err
	}
	if os.Getenv("TMUX") != "" {
		return exec.Command("tmux", "switch-client", "-t", "="+session).Run()
	}
	return execTmux("attach-session", "-t", "="+session)
}

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

// HasSession reports whether the tmux session exists.
func HasSession(session string) bool {
	return exec.Command("tmux", "has-session", "-t", "="+session).Run() == nil
}

// Open starts shellCmd (run by bash -c) in a new detached window name of
// session, in dir, creating the session if needed. A new session's tmux
// server doesn't get the variables in unset (shells opened there later
// would inherit them).
func Open(session, name, dir, shellCmd string, unset ...string) error {
	var cmd *exec.Cmd
	if HasSession(session) {
		cmd = exec.Command("tmux", "new-window", "-d", "-t", "="+session+":", "-n", name, "-c", dir, "bash", "-c", shellCmd)
	} else {
		cmd = exec.Command("tmux", "new-session", "-d", "-s", session, "-n", name, "-c", dir, "bash", "-c", shellCmd)
		for _, kv := range os.Environ() {
			keep := true
			for _, u := range unset {
				if strings.HasPrefix(kv, u+"=") {
					keep = false
				}
			}
			if keep {
				cmd.Env = append(cmd.Env, kv)
			}
		}
	}
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SendKeys types text into window name of session, then Enter.
func SendKeys(session, name, text string) error {
	return exec.Command("tmux", "send-keys", "-t", "="+session+":"+name, text, "Enter").Run()
}

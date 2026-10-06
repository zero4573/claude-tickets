package launcher

import (
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Tmux is the tmux backend: a group is a tmux session.
type Tmux struct{}

func (Tmux) Name() string { return "tmux" }

func (Tmux) Caps() Caps { return Caps{Background: true, List: true, SendKeys: true, Attach: true} }

func (Tmux) Windows(group string) []string {
	out, err := exec.Command("tmux", "list-windows", "-t", "="+group, "-F", "#W").Output()
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

func hasSession(group string) bool {
	return exec.Command("tmux", "has-session", "-t", "="+group).Run() == nil
}

// Open puts the environment on the command line: a window gets its
// environment from the tmux server, i.e. from whoever started it first.
func (Tmux) Open(w Window) error {
	argv := w.Argv
	if len(w.Env) > 0 {
		keys := make([]string, 0, len(w.Env))
		for k := range w.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		env := []string{"env"}
		for _, k := range keys {
			env = append(env, k+"="+w.Env[k])
		}
		argv = append(env, argv...)
	}
	shellCmd := ShellJoin(argv) + "; echo; read -rp 'Session ended. Press Enter to close this window. '"
	var cmd *exec.Cmd
	if hasSession(w.Group) {
		cmd = exec.Command("tmux", "new-window", "-d", "-t", "="+w.Group+":", "-n", w.Name, "-c", w.Dir, "bash", "-c", shellCmd)
	} else {
		cmd = exec.Command("tmux", "new-session", "-d", "-s", w.Group, "-n", w.Name, "-c", w.Dir, "bash", "-c", shellCmd)
		for _, kv := range os.Environ() {
			keep := true
			for _, u := range w.Unset {
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

func (Tmux) SendKeys(group, name, text string) error {
	return exec.Command("tmux", "send-keys", "-t", "="+group+":"+name, text, "Enter").Run()
}

// Attach outside tmux replaces the process where the OS can, so it returns
// only on failure.
func (Tmux) Attach(group, name string) error {
	if err := exec.Command("tmux", "select-window", "-t", "="+group+":"+name).Run(); err != nil {
		return err
	}
	if os.Getenv("TMUX") != "" {
		return exec.Command("tmux", "switch-client", "-t", "="+group).Run()
	}
	return execTmux("attach-session", "-t", "="+group)
}

// execTmux replaces this process with tmux (so the terminal belongs to it),
// or runs it in the foreground where the OS has no exec.
func execTmux(args ...string) error {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	if execReplace(path, append([]string{"tmux"}, args...), os.Environ()) == nil {
		return nil
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// ShellJoin quotes argv for a POSIX shell.
func ShellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		if a != "" && strings.Trim(a, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-./=:,@+%") == "" {
			q[i] = a
		} else {
			q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(q, " ")
}

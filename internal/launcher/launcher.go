// Package launcher runs ticket sessions in the foreground, in the terminal
// tix was started from (Run): tix is replaced by the session where the OS can
// exec (exec_unix.go), else runs it as a child (exec_windows.go).
package launcher

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"strings"
)

// Session is a session to run.
type Session struct {
	Dir  string
	Argv []string
	// Env is set for the session
	Env map[string]string
	// PathFirst (when not "") goes first on the PATH the session starts
	// with: a dev workspace's built tix
	PathFirst string
}

// Run runs w in this terminal, in the foreground: cwd w.Dir, the
// environment with w.Env set and w.PathFirst first on its PATH.
// Where the OS can exec, tix is replaced by w's program and Run returns only
// on failure. Elsewhere (Windows) the program runs as a child with tix's
// stdio, tix ignores Ctrl-C until it exits (the console sends it to both, so
// the program handles it and tix isn't killed first), and its
// *exec.ExitError is returned.
func Run(w Session) error {
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
	env := mergeEnv(os.Environ(), withPath(w.Env, w.PathFirst, os.Getenv("PATH")))
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

// withPath is env with PATH set to first, then path (the PATH the session
// starts with), when first isn't "".
func withPath(env map[string]string, first, path string) map[string]string {
	if first == "" {
		return env
	}
	out := make(map[string]string, len(env)+1)
	for k, v := range env {
		out[k] = v
	}
	out["PATH"] = first
	if path != "" {
		out["PATH"] = first + string(os.PathListSeparator) + path
	}
	return out
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

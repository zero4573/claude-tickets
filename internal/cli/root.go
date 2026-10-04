// Package cli is the `ct` command tree.
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// Root builds the ct command.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "ct",
		Short: "Claude Code ticket workflow, with an Obsidian vault as long-term memory",
		Long: `ct: parallel Claude Code sessions that work tickets end to end, with an
Obsidian vault as their long-term memory. Every command acts on the current
vault (ct vault default; else the only vault).`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		vaultCmd(),
		newCmd(),
		startCmd(),
		feedbackCmd(),
		statusCmd(),
		attachCmd(),
		openCmd(),
		syncCmd(),
		wsCmd(),
		graphCmd(),
		kbCmd(),
		claudeCmd(),
		layoutCmd(),
	)
	return root
}

// Main runs ct and exits with its status.
func Main() {
	if err := Root().Execute(); err != nil {
		if e, ok := err.(exitError); ok {
			os.Exit(int(e))
		}
		fmt.Fprintln(os.Stderr, "ct: "+err.Error())
		os.Exit(1)
	}
}

// exitError ends ct with a status and no message (the command printed it).
type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit %d", int(e)) }

// --- commands not ported to Go yet: they run the bash scripts in libexec ---

// libexecDir holds the bash implementations: $CLAUDE_TICKETS_LIBEXEC, else
// ../libexec/claude-tickets next to the ct binary.
func libexecDir() string {
	if d := os.Getenv("CLAUDE_TICKETS_LIBEXEC"); d != "" {
		return d
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Join(filepath.Dir(exe), "..", "libexec", "claude-tickets")
}

// runScript replaces ct with the bash script name (from libexec, which
// also goes first on PATH so the scripts find each other).
func runScript(name string, args []string) error {
	dir := libexecDir()
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%s isn't available (looked in %s; set CLAUDE_TICKETS_LIBEXEC)", name, dir)
	}
	// The scripts find each other on PATH, and any ct they run finds them
	// through CLAUDE_TICKETS_LIBEXEC
	env := []string{"CLAUDE_TICKETS_LIBEXEC=" + dir}
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "PATH="):
			kv = "PATH=" + dir + string(os.PathListSeparator) + kv[5:]
		case strings.HasPrefix(kv, "CLAUDE_TICKETS_LIBEXEC="):
			continue
		}
		env = append(env, kv)
	}
	if err := execReplace(path, append([]string{name}, args...), env); err != nil {
		// No exec (e.g. Windows): run it and pass its status on
		cmd := exec.Command(path, args...)
		cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = env, os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return exitError(ee.ExitCode())
			}
			return err
		}
	}
	return nil
}

// scriptCmd is a subcommand still implemented by a bash script: every
// argument (and --help) goes to the script, after the lead arguments.
func scriptCmd(use, short, script string, lead ...string) *cobra.Command {
	return &cobra.Command{
		Use:                use,
		Short:              short,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScript(script, append(append([]string{}, lead...), args...))
		},
	}
}

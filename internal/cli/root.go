package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/zero4573/tix/internal/config"
	"github.com/zero4573/tix/internal/platform"
	"github.com/zero4573/tix/internal/version"
)

func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "tix",
		Short: "Claude Code ticket workflow, with an Obsidian vault as long-term memory",
		Long: `tix: parallel Claude Code sessions that work tickets end to end, with an
Obsidian vault as their long-term memory. Every command acts on the current
vault (tix vault default; else the only vault).`,
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		vaultCmd(),
		newCmd(),
		startCmd(),
		feedbackCmd(),
		statusCmd(),
		openCmd(),
		syncCmd(),
		wsCmd(),
		cleanCmd(),
		specCmd(),
		teachCmd(),
		claudeCmd(),
		layoutCmd(),
		hookCmd(),
		dirCmd(),
		ticketCmd(),
		relayCmd(),
		citeCmd(),
	)
	return root
}

func Main() {
	// An install from before the rename (claude-tickets): its config,
	// cache and logs move to tix's folders on the first run
	for _, m := range platform.MigrateLegacy(runtime.GOOS, os.Getenv, platform.Home()) {
		if m.Err != nil {
			fmt.Fprintf(os.Stderr, "tix: couldn't move %s to %s (%v); move it yourself\n", m.From, m.To, m.Err)
		} else {
			fmt.Fprintf(os.Stderr, "tix: moved %s to %s (the old claude-tickets folder)\n", m.From, m.To)
		}
	}
	if err := Root().Execute(); err != nil {
		var quiet exitError
		if errors.As(err, &quiet) {
			os.Exit(int(quiet))
		}
		fmt.Fprintln(os.Stderr, "tix: "+err.Error())
		var coded codeError
		if errors.As(err, &coded) {
			os.Exit(coded.code)
		}
		os.Exit(1)
	}
}

// exitError ends tix with a status and no message (the command printed it).
type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit %d", int(e)) }

type codeError struct {
	code int
	err  error
}

func (e codeError) Error() string { return e.err.Error() }

// exitWith makes err end tix with status code (its message still printed).
func exitWith(code int, err error) error { return codeError{code, err} }

// jsonUnmarshal decodes JSON keeping numbers as written (json.Number).
func jsonUnmarshal(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}

// logPath is a command's log as help texts show it (~/...).
func logPath(name string) string {
	return config.TildePath(filepath.Join(config.StateDir(), name+".log"))
}

// Package cli is the `ct` command tree.
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

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
		hookCmd(),
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
		if e, ok := err.(codeError); ok {
			os.Exit(e.code)
		}
		os.Exit(1)
	}
}

// exitError ends ct with a status and no message (the command printed it).
type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit %d", int(e)) }

// codeError is an error that ends ct with a given status.
type codeError struct {
	code int
	err  error
}

func (e codeError) Error() string { return e.err.Error() }

// exitWith makes err end ct with status code (its message still printed).
func exitWith(code int, err error) error { return codeError{code, err} }

// jsonUnmarshal decodes JSON keeping numbers as written (json.Number).
func jsonUnmarshal(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}


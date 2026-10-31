package cli

import "github.com/spf13/cobra"

// usageArgs wraps a positional-args check so a usage error ends tix with
// status 2, as the vault search commands' contract wants (0 ok, 1 error,
// 2 usage / not found / ambiguous, 3 refused).
func usageArgs(fn cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := fn(cmd, args); err != nil {
			return exitWith(2, err)
		}
		return nil
	}
}

// usageExit makes cmd's argument and flag errors end tix with status 2.
func usageExit(cmd *cobra.Command) *cobra.Command {
	if cmd.Args != nil {
		cmd.Args = usageArgs(cmd.Args)
	}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return exitWith(2, err) })
	return cmd
}

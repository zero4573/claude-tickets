package cli

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

func TestUsageExit(t *testing.T) {
	cmd := usageExit(&cobra.Command{Use: "x", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { return nil }})
	var coded codeError
	if err := cmd.Args(cmd, []string{"a"}); !errors.As(err, &coded) || coded.code != 2 {
		t.Fatalf("args error = %v, want status 2", err)
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Fatalf("no args: %v", err)
	}
	if err := cmd.FlagErrorFunc()(cmd, errors.New("bad flag")); !errors.As(err, &coded) || coded.code != 2 {
		t.Fatalf("flag error = %v, want status 2", err)
	}
}

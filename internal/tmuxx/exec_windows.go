//go:build windows

package tmuxx

import (
	"os"
	"os/exec"
)

// execTmux runs tmux in the foreground (Windows has no exec).
func execTmux(args ...string) error {
	cmd := exec.Command("tmux", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

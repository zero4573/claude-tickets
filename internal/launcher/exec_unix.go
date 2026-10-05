//go:build !windows

package launcher

import (
	"os"
	"os/exec"
	"syscall"
)

// execTmux replaces this process with tmux (so the terminal belongs to it).
func execTmux(args ...string) error {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	return syscall.Exec(path, append([]string{"tmux"}, args...), os.Environ())
}

//go:build darwin

package proc

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// sZomb is SZOMB from <sys/proc.h> (x/sys doesn't define it).
const sZomb = 5

// identity is darwin:<start time>, from the kern.proc.pid sysctl.
func identity(pid int) (string, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp.Proc.P_pid == 0 || kp.Proc.P_stat == sZomb {
		return "", ErrNotRunning
	}
	st := kp.Proc.P_starttime
	return fmt.Sprintf("darwin:%d.%06d", st.Sec, st.Usec), nil
}

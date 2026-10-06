// Package proc tells whether a process is still the one ct recorded: a
// PID plus an identity (its start time), so a PID the OS has since given
// to another process doesn't count. The OS differences live in the
// build-tagged proc_<os>.go files.
package proc

import "errors"

// ErrNotRunning: no process has the PID (or it's a zombie).
var ErrNotRunning = errors.New("no such process")

// ErrUnsupported: this OS (or this process's permissions) can't tell.
var ErrUnsupported = errors.New("process identity not supported here")

// Identity is an opaque string unique to a running process: its start time
// (plus the boot id on Linux), so a reused PID gives a different value.
// Exec keeps it, so ct's identity just before exec is the new program's.
func Identity(pid int) (string, error) {
	if pid <= 0 {
		return "", ErrNotRunning
	}
	return identity(pid)
}

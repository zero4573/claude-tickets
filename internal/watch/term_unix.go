//go:build !windows

package watch

import (
	"os"
	"os/signal"
	"syscall"
)

// EnableVT makes f's console process escape sequences; Unix terminals
// always do, so there's nothing to change or restore.
func EnableVT(f *os.File) (restore func(), err error) { return func() {}, nil }

// ResizeSignals is a channel that fires when the terminal is resized
// (SIGWINCH), and a func to stop it.
func ResizeSignals() (<-chan os.Signal, func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	return ch, func() { signal.Stop(ch) }
}

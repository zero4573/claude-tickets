//go:build windows

package watch

import (
	"os"

	"golang.org/x/sys/windows"
)

// EnableVT turns on escape sequence processing for f's console (Windows 10
// and later) and returns a func putting its old mode back; an error means
// the console can't (an older Windows).
func EnableVT(f *os.File) (restore func(), err error) {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return nil, err
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING == 0 {
		if err := windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
			return nil, err
		}
	}
	return func() { _ = windows.SetConsoleMode(h, mode) }, nil
}

// ResizeSignals has no signal on Windows (Run polls the size instead): a
// nil channel, which never fires.
func ResizeSignals() (<-chan os.Signal, func()) { return nil, func() {} }

//go:build windows

package proc

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// stillActive is STILL_ACTIVE, GetExitCodeProcess's code for a live
// process (x/sys doesn't define it).
const stillActive = 259

// identity is windows:<creation time>, from GetProcessTimes.
func identity(pid int) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return "", ErrNotRunning
	}
	if err != nil {
		return "", ErrUnsupported
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return "", ErrUnsupported
	}
	if code != stillActive {
		return "", ErrNotRunning
	}
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return "", ErrUnsupported
	}
	return fmt.Sprintf("windows:%d", created.Nanoseconds()), nil
}

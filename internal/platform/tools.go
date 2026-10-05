package platform

import (
	"os/exec"
	"runtime"
)

// Notify shows a desktop notification, when the OS has a way to: a
// failure (or no notifier) is silent.
func Notify(app, title, body string, urgent bool) {
	if argv := notifyCommand(runtime.GOOS, app, title, body, urgent); argv != nil {
		if path, err := exec.LookPath(argv[0]); err == nil {
			_ = exec.Command(path, argv[1:]...).Run()
		}
	}
}

func notifyCommand(goos, app, title, body string, urgent bool) []string {
	switch goos {
	case "windows":
		return nil // TODO: a toast through PowerShell
	case "darwin":
		return []string{"osascript", "-e", "on run argv\ndisplay notification (item 2 of argv) with title (item 1 of argv)\nend run", title, body}
	default:
		argv := []string{"notify-send", "-a", app}
		if urgent {
			argv = append(argv, "-u", "critical")
		}
		return append(argv, title, body)
	}
}

func OpenCommand(target string) []string { return openCommand(runtime.GOOS, target) }

func openCommand(goos, target string) []string {
	switch goos {
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", target}
	case "darwin":
		return []string{"open", target}
	default:
		return []string{"xdg-open", target}
	}
}

// ShellCommand is the command that runs a command line: bash -c (sh
// when there's no bash), cmd /C on Windows.
func ShellCommand(line string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/C", line}
	}
	if _, err := exec.LookPath("bash"); err == nil {
		return []string{"bash", "-c", line}
	}
	return []string{"sh", "-c", line}
}

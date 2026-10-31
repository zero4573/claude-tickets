package platform

import (
	"strings"
	"testing"
)

func TestCommands(t *testing.T) {
	if got := strings.Join(notifyCommand("linux", "tix sync", "done", "b", Critical), " "); got != "notify-send -a tix sync -u critical done b" {
		t.Error(got)
	}
	if got := strings.Join(notifyCommand("linux", "tix", "t", "b", Sticky), " "); got != "notify-send -a tix -u critical -t 0 t b" {
		t.Error(got)
	}
	if got := notifyCommand("darwin", "tix", "t", "b", Normal); got[0] != "osascript" || got[3] != "t" || got[4] != "b" {
		t.Error(got)
	}
	if notifyCommand("windows", "tix", "t", "b", Normal) != nil {
		t.Error("windows: no notifier yet")
	}
	for goos, want := range map[string]string{"linux": "xdg-open", "darwin": "open", "windows": "rundll32"} {
		if got := openCommand(goos, "x")[0]; got != want {
			t.Errorf("%s: %s", goos, got)
		}
	}
}

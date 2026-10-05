package container

import "testing"

func TestContainerPath(t *testing.T) {
	for _, tc := range []struct{ goos, host, want string }{
		{"linux", "/home/jane/work", "/home/jane/work"},
		{"darwin", "/Users/jane/work", "/Users/jane/work"},
		{"windows", `C:\Users\jane\work`, "/mnt/c/Users/jane/work"},
		{"windows", `\\server\share`, "//server/share"},
	} {
		if got := containerPath(tc.goos, tc.host); got != tc.want {
			t.Errorf("%s %s: %s, want %s", tc.goos, tc.host, got, tc.want)
		}
	}
}

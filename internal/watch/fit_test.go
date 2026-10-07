package watch

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func numbered(n int) []string {
	var lines []string
	for i := 1; i <= n; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	return lines
}

func TestFitWidth(t *testing.T) {
	got := Fit([]string{"abcdefghij", "short", "ééééééé", ""}, 5, 10)
	want := []string{"abcde", "short", "ééééé", ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFitHeight(t *testing.T) {
	cases := []struct {
		name          string
		lines, height int
		want          []string
	}{
		{"fits", 5, 5, numbered(5)},
		{"fewer", 3, 5, numbered(3)},
		{"overflow", 8, 5, append(numbered(4), "... 4 more")},
		{"one more", 6, 5, append(numbered(4), "... 2 more")},
		{"height 1", 3, 1, numbered(1)},
		{"height 2", 3, 2, []string{"line 1", "... 2 more"}},
		{"height 0 is unknown: 24", 30, 0, append(numbered(23), "... 7 more")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Fit(numbered(c.lines), 80, c.height); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestFitUnknownWidth(t *testing.T) {
	long := string(make([]byte, 100))
	got := Fit([]string{long}, 0, 0)
	if n := len(got[0]); n != 80 {
		t.Errorf("unknown width: line of %d, want 80", n)
	}
}

func TestFitMoreLineCut(t *testing.T) {
	got := Fit(numbered(20), 6, 3)
	if want := []string{"line 1", "line 2", "... 18"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFitSanitize(t *testing.T) {
	got := Fit([]string{"a\x1b[2Jb\rc\td\x7fe\u009bf"}, 80, 24)
	if want := "a [2Jb c d e f"; got[0] != want {
		t.Errorf("got %q, want %q", got[0], want)
	}
}

func TestShortDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		2 * time.Second:         "2s",
		time.Minute:             "1m",
		90 * time.Second:        "1m30s",
		time.Hour:               "1h",
		time.Hour + time.Minute: "1h1m",
		1500 * time.Millisecond: "1.5s",
	} {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

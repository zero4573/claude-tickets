package links

import (
	"reflect"
	"testing"
)

func TestTargets(t *testing.T) {
	text := "---\ndepends-on: [[dep]]\n---\n" +
		"See [[a]], [[projects/acme/b.md#h|alias]] and ![[c]] and [[a]] again.\n" +
		"Code `[[inline]]` is skipped.\n```\n[[fenced]]\n```\n[[d\\|x]]\n"
	got := Targets(text)
	want := []string{"dep", "a", "b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if Targets("no links") != nil {
		t.Fatal("expected nil")
	}
}

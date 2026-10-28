package jira

import "testing"

func TestTaskListForms(t *testing.T) {
	in := "- [ ] a\n- [x] b\n- [X] c\n* [ ] d\n+ [ ] e\n- [] f\n[] g\n[x] h\nplain [ ] text\n[x]: http://u\n1. [ ] i\n2) [x] j\n> - [x] k\n> > [] l"
	want := "- \\[ \\] a\n- \\[x\\] b\n- \\[x\\] c\n- \\[ \\] d\n- \\[ \\] e\n- \\[ \\] f\n- \\[ \\] g\n[x] h\nplain [ ] text\n[x]: http://u\n1. \\[ \\] i\n2) \\[x\\] j\n> - \\[x\\] k\n> > - \\[ \\] l"
	if got := plainTaskItems(in); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestTaskListNesting(t *testing.T) {
	in := "- [ ] a\n  - [x] b\n    * [ ] c"
	want := "- \\[ \\] a\n  - \\[x\\] b\n    - \\[ \\] c"
	if got := plainTaskItems(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTaskListCodeFence(t *testing.T) {
	in := "- [ ] a\n```\n- [ ] x\n```\n~~~\n- [x] y\n~~~\n- [x] b"
	want := "- \\[ \\] a\n```\n- [ ] x\n```\n~~~\n- [x] y\n~~~\n- \\[x\\] b"
	if got := plainTaskItems(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTaskListIdempotent(t *testing.T) {
	once := plainTaskItems("- [ ] a\n  - [x] b\ntext")
	if twice := plainTaskItems(once); twice != once {
		t.Fatalf("not idempotent: %q vs %q", once, twice)
	}
}

func TestTaskListLongFence(t *testing.T) {
	in := "````\n```\n- [ ] x\n```\n````\n- [ ] a\n```go\n- [ ] y\n```\n"
	want := "````\n```\n- [ ] x\n```\n````\n- \\[ \\] a\n```go\n- [ ] y\n```\n"
	if got := plainTaskItems(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

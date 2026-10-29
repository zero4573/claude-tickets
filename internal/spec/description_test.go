package spec

import "testing"

func TestReplaceChange(t *testing.T) {
	body := "### acme-x · orders\n\nRemoved: REQ-1\n"
	sec := "## Spec change\n\n" + body
	cases := []struct{ name, in, want string }{
		{"append", "Intro.\n", "Intro.\n\n" + sec},
		{"append no newline", "Intro.", "Intro.\n\n" + sec},
		{"append blank end", "Intro.\n\n", "Intro.\n\n" + sec},
		{"append empty", "", sec},
		{"replace at end", "Intro.\n\n## Spec change\n\nold\n", "Intro.\n\n" + sec},
		{"replace at end no newline", "Intro.\n\n## Spec change\nold", "Intro.\n\n" + sec},
		{"replace with following", "A\n\n## Spec change\nold\nmore\n\n## Notes\nkeep\n", "A\n\n" + sec + "\n## Notes\nkeep\n"},
		{"renamed heading appended", "## Spec changes\nold\n", "## Spec changes\nold\n\n" + sec},
		{"fenced heading ignored", "```\n## Spec change\n```\n", "```\n## Spec change\n```\n\n" + sec},
		{"crlf replace", "A\r\n\r\n## Spec change\r\nold\r\n\r\n## Notes\r\nkeep\r\n",
			"A\r\n\r\n## Spec change\r\n\r\n### acme-x · orders\r\n\r\nRemoved: REQ-1\r\n\r\n## Notes\r\nkeep\r\n"},
		{"crlf append", "A\r\n", "A\r\n\r\n## Spec change\r\n\r\n### acme-x · orders\r\n\r\nRemoved: REQ-1\r\n"},
	}
	for _, c := range cases {
		if got := ReplaceChange(c.in, body); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
	if got := ReplaceChange("A\n", ""); got != "A\n\n## Spec change\n\nNo spec change.\n" {
		t.Errorf("empty body: %q", got)
	}
	once := ReplaceChange("A\n\n## Spec change\nold\n\n## N\nx\n", body)
	if ReplaceChange(once, body) != once {
		t.Error("not idempotent")
	}
}

func TestCurrentChange(t *testing.T) {
	if _, ok := CurrentChange("A\n"); ok {
		t.Error("found in none")
	}
	got, ok := CurrentChange("A\n## Spec change\nold\n## B\n")
	if !ok || got != "## Spec change\nold\n" {
		t.Errorf("got %q %v", got, ok)
	}
}

func TestHasChange(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"Intro.\n", false},
		{"## Spec change\n\nNo spec change.\n", false},
		{"## Spec change\n\n\n## Notes\nx\n", false},
		{"## Spec change\n\n### a · b\n\nRemoved: REQ-1\n", true},
		{"A\n## Spec change\r\nold\r\n## Notes\r\n", true},
		{"```\n## Spec change\nold\n```\n", false},
	}
	for _, c := range cases {
		if got := HasChange(c.in); got != c.want {
			t.Errorf("HasChange(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

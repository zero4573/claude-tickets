package mdsec

import (
	"errors"
	"strings"
	"testing"
)

func titles(d *Doc) []string {
	var out []string
	for _, s := range d.Sections {
		out = append(out, strings.Join(s.Path, ">"))
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNoFrontmatter(t *testing.T) {
	d := Parse([]byte("pre\n# A\nbody\n"))
	if d.Frontmatter != nil {
		t.Fatal("unexpected frontmatter")
	}
	eq(t, titles(d), []string{"", "A"})
	if d.Sections[0].Level != 0 || d.Sections[0].Body != (Span{0, 4}) {
		t.Fatalf("preamble %+v", d.Sections[0])
	}
}

func TestFrontmatterOnly(t *testing.T) {
	src := "---\na: 1\n---\n"
	d := Parse([]byte(src))
	if d.Frontmatter == nil || *d.Frontmatter != (Span{0, len(src)}) {
		t.Fatalf("fm %+v", d.Frontmatter)
	}
	if len(d.Sections) != 0 {
		t.Fatalf("sections %+v", d.Sections)
	}
}

func TestUnterminatedFrontmatter(t *testing.T) {
	d := Parse([]byte("---\na: 1\n# H\n"))
	if d.Frontmatter != nil {
		t.Fatal("frontmatter should be nil")
	}
	eq(t, titles(d), []string{"", "H"})
}

func TestHeadingInFrontmatterIgnored(t *testing.T) {
	d := Parse([]byte("---\n# not\n---\n# Real\n"))
	eq(t, titles(d), []string{"Real"})
	if d.Sections[0].StartLine != 4 {
		t.Fatalf("line %d", d.Sections[0].StartLine)
	}
}

func TestFences(t *testing.T) {
	src := "# A\n```\n# no\n~~~\n# still no\n````\n# B\n~~~~\n# no\n~~~\n# no2\n~~~~~\n# C\n```\n# unterminated\n"
	d := Parse([]byte(src))
	eq(t, titles(d), []string{"A", "B", "C"})
	last := d.Sections[2]
	if last.Whole.End != len(src) {
		t.Fatal("unterminated fence should run to EOF")
	}
}

func TestNotHeadings(t *testing.T) {
	d := Parse([]byte("#tag\n####### seven\n    # indented\n# \n#\n## Real\n"))
	// "# " and "#" are empty-title headings; the rest are body text
	var real []string
	for _, s := range d.Sections {
		if s.Level > 0 {
			real = append(real, s.Title)
		}
	}
	eq(t, real, []string{"", "", "Real"})
}

func TestSkipLevelsAndDuplicates(t *testing.T) {
	src := "# P1\n### Deep\n## Notes\n# P2\n## Notes\n"
	d := Parse([]byte(src))
	eq(t, titles(d), []string{"P1", "P1>Deep", "P1>Notes", "P2", "P2>Notes"})
	if _, err := d.Find("Notes"); err == nil {
		t.Fatal("want ambiguous")
	} else {
		var ae *AmbiguousError
		if !errors.As(err, &ae) || len(ae.Candidates) != 2 {
			t.Fatalf("err %v", err)
		}
		if !strings.Contains(err.Error(), "P1 > Notes (line 3)") || !strings.Contains(err.Error(), "P2 > Notes (line 5)") {
			t.Fatalf("msg %q", err.Error())
		}
	}
	s, err := d.Find("P2 > Notes")
	if err != nil || s.StartLine != 5 {
		t.Fatalf("%v %+v", err, s)
	}
	if s, err = d.Find("### Deep"); err != nil || s.Title != "Deep" {
		t.Fatalf("%v", err)
	}
	if _, err = d.Find("## Deep"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("level check: %v", err)
	}
	if _, err = d.Find("Nope"); !errors.Is(err, ErrNotFound) {
		t.Fatal("want not found")
	}
}

func TestTrailingHashes(t *testing.T) {
	d := Parse([]byte("## Title ##\n## C# ##\n## C#\n# x #   \n"))
	var got []string
	for _, s := range d.Sections {
		got = append(got, s.Title)
	}
	eq(t, got, []string{"Title", "C#", "C#", "x"})
}

func TestCRLFAndNoFinalNewline(t *testing.T) {
	src := "---\r\na: 1\r\n---\r\n# A\r\nbody\r\n## B\r\nlast"
	d := Parse([]byte(src))
	if d.Frontmatter == nil || d.Frontmatter.End != 16 {
		t.Fatalf("fm %+v", d.Frontmatter)
	}
	eq(t, titles(d), []string{"A", "A>B"})
	a, b := d.Sections[0], d.Sections[1]
	if a.Title != "A" || a.Heading != (Span{16, 21}) {
		t.Fatalf("%+v", a)
	}
	if b.Whole.End != len(src) || b.EndLine != 7 || a.EndLine != 7 || b.StartLine != 6 {
		t.Fatalf("%+v %+v", a, b)
	}
	if string(src[b.Body.Start:b.Body.End]) != "last" {
		t.Fatalf("body %q", src[b.Body.Start:b.Body.End])
	}
}

func TestEmptySections(t *testing.T) {
	src := "# A\n## B\n# C"
	d := Parse([]byte(src))
	a := d.Sections[0]
	if a.Body.Start != a.Body.End || a.Whole != (Span{0, 9}) || a.EndLine != 2 {
		t.Fatalf("%+v", a)
	}
	c := d.Sections[2]
	if c.Heading != (Span{9, 12}) || c.Body.Start != c.Body.End || c.StartLine != 3 || c.EndLine != 3 {
		t.Fatalf("%+v", c)
	}
}

func TestCoverage(t *testing.T) {
	fixtures := []string{
		"", "plain\n", "---\nx: 1\n---\n", "---\nx: 1\n---\npre\n# A\n## B\ntext\n# C\n",
		"# A\n```\n# x\n```\n## B\n### C\n## D\n", "pre\n## A\n# B\n## C\r\nend",
		"## First\n# Second\n### Deep\n",
	}
	for _, f := range fixtures {
		d := Parse([]byte(f))
		pos := 0
		if d.Frontmatter != nil {
			pos = d.Frontmatter.End
		}
		for _, s := range d.Sections {
			if s.Whole.Start < pos {
				continue // nested inside the previous top-level section
			}
			if s.Whole.Start != pos {
				t.Fatalf("%q: gap/overlap at %d, section %+v", f, pos, s)
			}
			pos = s.Whole.End
		}
		if pos != len(f) {
			t.Fatalf("%q: covered %d of %d", f, pos, len(f))
		}
	}
}

func TestHTMLCommentHeadings(t *testing.T) {
	d := Parse([]byte("# A\n<!--\n## hidden\n-->\n## B\n<!-- one --> x\n## C\n<!-- open\n# no\n"))
	eq(t, titles(d), []string{"A", "A>B", "A>C"})
	if _, err := d.Find("hidden"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestWholeTitleBeforeSplit(t *testing.T) {
	d := Parse([]byte("# X > Y\n## Z\n# X\n## Y\n"))
	s, err := d.Find("X > Y")
	if err != nil || s.Level != 1 || s.Title != "X > Y" {
		t.Fatal(s, err)
	}
	s, err = d.Find("## Y")
	if err != nil || s.Level != 2 || s.Path[0] != "X" {
		t.Fatal(s, err)
	}
}

func TestWhitespacePreamble(t *testing.T) {
	d := Parse([]byte("---\na: 1\n---\n\n\n# A\nx\n"))
	eq(t, titles(d), []string{"A"})
	d = Parse([]byte("\n  \n"))
	if len(d.Sections) != 0 {
		t.Fatal(titles(d))
	}
}

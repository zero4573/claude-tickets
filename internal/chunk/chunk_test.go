package chunk

import (
	"strings"
	"testing"

	"github.com/zero4573/claude-tickets/internal/mdsec"
)

func checkSpans(t *testing.T, src string, cs []Chunk) {
	t.Helper()
	for _, c := range cs {
		first := strings.SplitN(c.Text, "\n", 2)[0]
		if !strings.Contains(src[c.Start:c.End], first) {
			t.Fatalf("span %q does not contain %q", src[c.Start:c.End], first)
		}
		if c.Tokens != Tokens(c.Text) {
			t.Fatalf("tokens %d", c.Tokens)
		}
		lines := strings.Split(src, "\n")
		if !strings.Contains(lines[c.StartLine-1], first) {
			t.Fatalf("start line %d = %q, want %q", c.StartLine, lines[c.StartLine-1], first)
		}
		if c.EndLine < c.StartLine || c.EndLine > len(lines) {
			t.Fatalf("end line %d", c.EndLine)
		}
	}
}

func TestTokens(t *testing.T) {
	if Tokens("") != 0 || Tokens("a") != 1 || Tokens("abcd") != 1 || Tokens("abcde") != 2 {
		t.Fatal("estimate")
	}
}

func TestBasic(t *testing.T) {
	src := "---\ntitle: x\n---\nintro text\n# A\nalpha\n\n\n\nbeta\n## B\n# Empty\n## C\ngamma\n"
	cs := Split([]byte(src), Options{})
	if len(cs) != 3 {
		t.Fatalf("%d chunks: %+v", len(cs), cs)
	}
	if len(cs[0].Heading) != 0 || cs[0].Text != "intro text" {
		t.Fatalf("preamble %+v", cs[0])
	}
	if strings.Join(cs[1].Heading, ">") != "A" || cs[1].Text != "alpha\n\nbeta" {
		t.Fatalf("A %+v", cs[1])
	}
	if strings.Join(cs[2].Heading, ">") != "Empty>C" || cs[2].Text != "gamma" {
		t.Fatalf("C %+v", cs[2])
	}
	for _, c := range cs {
		if strings.Contains(c.Text, "title: x") {
			t.Fatal("frontmatter leaked")
		}
	}
	checkSpans(t, src, cs)
	if cs[0].StartLine != 4 || cs[1].StartLine != 6 || cs[1].EndLine != 10 {
		t.Fatalf("lines %+v", cs[:2])
	}
}

func TestCommentsAndLinks(t *testing.T) {
	src := "# A\n<!-- gone -->\nkeep <!-- inline --> this\n<!--\nmulti\n\nline\n-->\nsee ![[embed]] and [[link|alias]]\n"
	cs := Split([]byte(src), Options{})
	if len(cs) != 1 {
		t.Fatalf("%+v", cs)
	}
	x := cs[0].Text
	if strings.Contains(x, "gone") || strings.Contains(x, "inline") || strings.Contains(x, "multi") {
		t.Fatalf("comment kept: %q", x)
	}
	for _, w := range []string{"keep", "this", "embed", "link", "alias"} {
		if !strings.Contains(x, w) {
			t.Fatalf("lost %q in %q", w, x)
		}
	}
	if !strings.Contains(src[cs[0].Start:cs[0].End], "keep") {
		t.Fatal("span")
	}
}

func para(w string, n int) string { return strings.TrimSpace(strings.Repeat(w+" ", n)) }

func TestSplitParagraphs(t *testing.T) {
	src := "# A\n" + para("aaaa", 30) + "\n\n" + para("bbbb", 30) + "\n\n" + para("cccc", 30) + "\n"
	cs := Split([]byte(src), Options{MaxTokens: 50})
	if len(cs) != 3 {
		t.Fatalf("%d", len(cs))
	}
	for _, c := range cs {
		if c.Tokens > 50 {
			t.Fatalf("over: %d", c.Tokens)
		}
	}
	checkSpans(t, src, cs)
}

func TestSplitListItems(t *testing.T) {
	var b strings.Builder
	b.WriteString("# L\n")
	for i := 0; i < 10; i++ {
		b.WriteString("- item " + para("word", 10) + "\n  continuation\n")
	}
	src := b.String()
	cs := Split([]byte(src), Options{MaxTokens: 40})
	if len(cs) < 5 {
		t.Fatalf("%d chunks", len(cs))
	}
	for _, c := range cs {
		if c.Tokens > 40 || !strings.HasPrefix(c.Text, "- item") {
			t.Fatalf("%+v", c)
		}
	}
	checkSpans(t, src, cs)
}

func TestCodeBlock(t *testing.T) {
	code := "```go\nfunc a() {}\n\n# not heading\nfunc b() {}\n```"
	src := "# A\nintro\n\n" + code + "\n\ntail\n"
	cs := Split([]byte(src), Options{})
	if len(cs) != 1 || !strings.Contains(cs[0].Text, code) {
		t.Fatalf("%+v", cs)
	}
	// code kept whole when the section is over max but the block fits
	cs = Split([]byte("# A\n"+para("x", 60)+"\n\n"+code+"\n"), Options{MaxTokens: 50})
	found := false
	for _, c := range cs {
		if strings.Contains(c.Text, code) {
			found = true
		}
	}
	if !found {
		t.Fatalf("code split: %+v", cs)
	}
	// split by lines when it alone exceeds the max
	var b strings.Builder
	b.WriteString("# A\n```\n")
	for i := 0; i < 40; i++ {
		b.WriteString("line number " + para("z", 5) + "\n")
	}
	b.WriteString("```\n")
	big := b.String()
	cs = Split([]byte(big), Options{MaxTokens: 60})
	if len(cs) < 5 {
		t.Fatalf("%d chunks", len(cs))
	}
	for _, c := range cs {
		if c.Tokens > 60 {
			t.Fatalf("over %d", c.Tokens)
		}
	}
	checkSpans(t, big, cs)
}

func TestCRLF(t *testing.T) {
	src := "# A\r\nfirst\r\n\r\nsecond\r\n"
	cs := Split([]byte(src), Options{})
	if len(cs) != 1 || cs[0].Text != "first\n\nsecond" {
		t.Fatalf("%q", cs[0].Text)
	}
}

func TestCoverage(t *testing.T) {
	src := "---\na: 1\n---\npre\n# A\nx\n## B\ny\n# C\nz"
	doc := mdsec.Parse([]byte(src))
	pos := doc.Frontmatter.End
	for _, s := range doc.Sections {
		if s.Whole.Start < pos {
			continue
		}
		if s.Whole.Start != pos {
			t.Fatalf("gap at %d", pos)
		}
		pos = s.Whole.End
	}
	if pos != len(src) {
		t.Fatal("not covered")
	}
	cs := Split([]byte(src), Options{})
	checkSpans(t, src, cs)
}

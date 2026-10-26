package embed_test

import (
	"os"
	"testing"

	"github.com/zero4573/claude-tickets/internal/embed"
	"github.com/zero4573/claude-tickets/internal/modelcache"
)

// TestRealLlama runs the real model; it needs CT_EMBED_REAL=1 and the files
// ct vault model fetch installs (CT_EMBED_LIB and CT_EMBED_MODEL override
// where they are).
func TestRealLlama(t *testing.T) {
	if os.Getenv("CT_EMBED_REAL") != "1" {
		t.Skip("set CT_EMBED_REAL=1 (after ct vault model fetch) to run")
	}
	if d, err := embed.RuntimeLibDir(); err != nil {
		t.Skipf("runtime libraries: %v (set CLAUDE_TICKETS_LIB_PATH to a directory holding them)", err)
	} else if d != "" {
		t.Setenv("CLAUDE_TICKETS_LIB_PATH", d)
	}
	m := modelcache.Default()
	lib, model := os.Getenv("CT_EMBED_LIB"), os.Getenv("CT_EMBED_MODEL")
	if lib == "" || model == "" {
		st := modelcache.StatusOf(m.ID)
		if !st.ModelOK || !st.LibOK {
			t.Skipf("model or lib not fetched (%s)", st.Problem)
		}
		lib, model = st.LibDir, st.ModelPath
	}
	e, err := embed.OpenLlama(lib, model, embed.LlamaOptions{ID: m.ID, QueryPrefix: m.QueryPrefix})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	vs, err := e.Embed([]string{
		"The cat sat on the mat.",
		"A feline was resting on the rug.",
		"Quarterly revenue grew by twelve percent.",
		"x",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 4 || len(vs[0]) != e.Dims() || e.Dims() != 384 {
		t.Fatalf("shape: %d vectors, dims %d", len(vs), e.Dims())
	}
	para, other := embed.Cosine(vs[0], vs[1]), embed.Cosine(vs[0], vs[2])
	t.Logf("paraphrase %.3f unrelated %.3f", para, other)
	if para <= other+0.1 {
		t.Errorf("paraphrase %.3f should beat unrelated %.3f", para, other)
	}
	if n := embed.Cosine(vs[0], vs[0]); n < 0.999 || n > 1.001 {
		t.Errorf("not normalised: %f", n)
	}
	// Batch and single calls agree.
	one, _ := e.Embed([]string{"A feline was resting on the rug."}, false)
	if c := embed.Cosine(one[0], vs[1]); c < 0.999 {
		t.Errorf("batched vs single cosine %f", c)
	}
	q, err := e.Embed([]string{"where is the cat"}, true)
	if err != nil || len(q) != 1 {
		t.Fatal(err)
	}
	// Longer than the context is truncated, not an error.
	long := make([]byte, 20000)
	for i := range long {
		long[i] = "abc def "[i%8]
	}
	if _, err := e.Embed([]string{string(long)}, false); err != nil {
		t.Fatal(err)
	}
}

package embed

import (
	"math"
	"testing"
)

func TestStub(t *testing.T) {
	e := Stub(64)
	if e.ID() != "stub" || e.Dims() != 64 {
		t.Fatalf("id %q dims %d", e.ID(), e.Dims())
	}
	texts := []string{
		"the deployment pipeline failed on staging",
		"staging deployment pipeline is failing",
		"recipe for chocolate cake with hazelnuts",
		"",
	}
	a, err := e.Embed(texts, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := e.Embed(texts, false)
	for i := range a {
		if len(a[i]) != 64 {
			t.Fatalf("dims %d", len(a[i]))
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				t.Fatal("not deterministic")
			}
		}
	}
	for i := 0; i < 3; i++ {
		var n float64
		for _, x := range a[i] {
			n += float64(x) * float64(x)
		}
		if math.Abs(n-1) > 1e-5 {
			t.Errorf("text %d not normalised: %f", i, n)
		}
	}
	if Cosine(a[0], a[1]) <= Cosine(a[0], a[2]) {
		t.Errorf("shared words should score higher: %f vs %f", Cosine(a[0], a[1]), Cosine(a[0], a[2]))
	}
	q, _ := e.Embed([]string{texts[0]}, true)
	if Cosine(q[0], a[0]) < 0.999 {
		t.Error("stub query vector should equal the document vector")
	}
}

func TestUnavailable(t *testing.T) {
	if _, err := OpenLlama(t.TempDir(), "/nonexistent/model.gguf", LlamaOptions{}); !isUnavailable(err) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

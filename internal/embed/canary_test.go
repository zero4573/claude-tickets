//go:build linux || darwin

package embed

import "testing"

func TestCheckLayout(t *testing.T) {
	good := llamaContextParams{NCtx: 512, NBatch: 2048, NUbatch: 512, NSeqMax: 1}
	if err := checkLayout(good); err != nil {
		t.Fatalf("good defaults rejected: %v", err)
	}
	bad := good
	bad.NBatch = 0
	if err := checkLayout(bad); !isUnavailable(err) {
		t.Fatalf("corrupted defaults accepted: %v", err)
	}
	bad = good
	bad.Embeddings = true
	if err := checkLayout(bad); !isUnavailable(err) {
		t.Fatalf("embeddings default not checked: %v", err)
	}
}

//go:build !linux && !darwin

package embed

import "fmt"

// OpenLlama is not supported on this platform.
func OpenLlama(libDir, modelPath string, o LlamaOptions) (Embedder, error) {
	return nil, fmt.Errorf("%w: llama.cpp is not supported on this platform", ErrUnavailable)
}

// depLibs: no libraries to preload where llama.cpp isn't supported
// (RuntimeLibDir still builds, and finds nothing to check).
var depLibs = []string{}

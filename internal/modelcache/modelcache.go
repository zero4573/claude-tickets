// Package modelcache pins, downloads and opens the local embedding model and
// the prebuilt llama.cpp libraries that run it. Files live in
// <cache>/models/ and are written only by Fetch, on the host.
package modelcache

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/embed"
)

//go:embed pins.json
var pinsJSON []byte

// libFile is one library extracted from a llama.cpp release archive: the
// archive member (a real file) and the name it is installed under (its
// soname, or the name the ggml backend loader looks for).
type libFile struct {
	Member string `json:"member"`
	As     string `json:"as"`
}

type asset struct {
	URL    string    `json:"url"`
	SHA256 string    `json:"sha256"`
	Dir    string    `json:"dir"` // top-level directory inside the archive
	Files  []libFile `json:"files"`
}

type modelPin struct {
	File        string `json:"file"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	Dims        int    `json:"dims"`
	QueryPrefix string `json:"query_prefix"`
	Default     bool   `json:"default"`
}

type pinFile struct {
	Llama struct {
		Version string           `json:"version"`
		Assets  map[string]asset `json:"assets"` // <goos>-<goarch>
	} `json:"llama"`
	Models map[string]modelPin `json:"models"`
}

var (
	pinOnce sync.Once
	pinned  pinFile
)

func pins() *pinFile {
	pinOnce.Do(func() {
		if err := json.Unmarshal(pinsJSON, &pinned); err != nil {
			panic("modelcache: pins.json: " + err.Error())
		}
	})
	return &pinned
}

// Model is one pinned embedding model.
type Model struct {
	ID, File, URL, SHA256, QueryPrefix string
	Dims                               int
}

func modelOf(p *pinFile, id string) (Model, bool) {
	m, ok := p.Models[id]
	if !ok {
		return Model{}, false
	}
	return Model{ID: id, File: m.File, URL: m.URL, SHA256: m.SHA256, QueryPrefix: m.QueryPrefix, Dims: m.Dims}, true
}

// Default is the model used when none is named: bge-small-en-v1.5, Q8_0 GGUF.
func Default() Model {
	p := pins()
	ids := make([]string, 0, len(p.Models))
	for id := range p.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if p.Models[id].Default {
			m, _ := modelOf(p, id)
			return m
		}
	}
	panic("modelcache: pins.json has no default model")
}

// IDs lists the pinned model ids, sorted.
func IDs() []string {
	var ids []string
	for id := range pins().Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Lookup finds a pinned model by id.
func Lookup(id string) (Model, bool) { return modelOf(pins(), id) }

// Dir is where models and libraries are cached.
func Dir() string { return filepath.Join(config.CacheDir(), "models") }

// ModelPath is where m's GGUF file lives (or will).
func ModelPath(m Model) string { return filepath.Join(Dir(), m.ID, m.File) }

func platformKey() string { return runtime.GOOS + "-" + runtime.GOARCH }

func libDirFor(p *pinFile) (string, error) {
	if _, ok := p.Llama.Assets[platformKey()]; !ok {
		return "", fmt.Errorf("no llama.cpp build is pinned for %s", platformKey())
	}
	return filepath.Join(Dir(), "llama-"+p.Llama.Version, platformKey()), nil
}

// LibDir is where this platform's llama.cpp libraries live (or will); an
// error when no build is pinned for the platform.
func LibDir() (string, error) { return libDirFor(pins()) }

// Status is what is present for one model.
type Status struct {
	Model             Model
	ModelOK, LibOK    bool
	ModelPath, LibDir string
	Problem           string
}

// StatusOf reports what is present for the model id; an unknown id is a
// Problem.
func StatusOf(id string) Status { return statusOf(pins(), id) }

func statusOf(p *pinFile, id string) Status {
	m, ok := modelOf(p, id)
	if !ok {
		return Status{Model: Model{ID: id}, Problem: fmt.Sprintf("unknown model %q (ct vault model list)", id)}
	}
	st := Status{Model: m, ModelPath: ModelPath(m)}
	var problems []string
	if fileOK(st.ModelPath) {
		st.ModelOK = true
	} else {
		problems = append(problems, "model not fetched")
	}
	dir, err := libDirFor(p)
	if err != nil {
		problems = append(problems, err.Error())
	} else {
		st.LibDir = dir
		st.LibOK = true
		for _, f := range p.Llama.Assets[platformKey()].Files {
			if !fileOK(filepath.Join(dir, f.As)) {
				st.LibOK = false
				break
			}
		}
		if !st.LibOK {
			problems = append(problems, "llama.cpp library not fetched")
		}
	}
	if len(problems) > 0 {
		st.Problem = strings.Join(problems, "; ") + " (on the host: ct vault model fetch)"
	}
	return st
}

func fileOK(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular() && st.Size() > 0
}

// Open returns the embedder for model id. $CLAUDE_TICKETS_EMBEDDER=stub gives
// embed.Stub(64) and =none an error wrapping embed.ErrUnavailable; otherwise
// the pinned library and model are loaded, or the error wraps
// embed.ErrUnavailable when they are missing.
func Open(id string) (embed.Embedder, error) {
	switch os.Getenv("CLAUDE_TICKETS_EMBEDDER") {
	case "stub":
		return embed.Stub(64), nil
	case "none":
		return nil, fmt.Errorf("%w: disabled by CLAUDE_TICKETS_EMBEDDER=none", embed.ErrUnavailable)
	}
	if _, ok := Lookup(id); !ok {
		return nil, fmt.Errorf("unknown model %q", id)
	}
	st := StatusOf(id)
	if !st.ModelOK || !st.LibOK {
		return nil, fmt.Errorf("%w: %s", embed.ErrUnavailable, st.Problem)
	}
	return embed.OpenLlama(st.LibDir, st.ModelPath, embed.LlamaOptions{ID: st.Model.ID, QueryPrefix: st.Model.QueryPrefix})
}

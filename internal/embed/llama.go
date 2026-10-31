//go:build linux || darwin

package embed

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// The struct layouts below mirror llama.h of the pinned llama.cpp build
// (modelcache/pins.json): purego passes them by value, so a different
// release needs them checked against its header.

type llamaModelParams struct {
	Devices                  unsafe.Pointer
	TensorBuftOverrides      unsafe.Pointer
	NGPULayers               int32
	SplitMode                int32
	LoadMode                 int32
	LazyMode                 int32
	MainGPU                  int32
	TensorSplit              unsafe.Pointer
	ProgressCallback         unsafe.Pointer
	ProgressCallbackUserData unsafe.Pointer
	KvOverrides              unsafe.Pointer
	VocabOnly                bool
	CheckTensors             bool
	UseExtraBufts            bool
	NoHost                   bool
	NoAlloc                  bool
	LoadMtp                  bool
}

type llamaContextParams struct {
	NCtx              uint32
	NBatch            uint32
	NUbatch           uint32
	NSeqMax           uint32
	NRsSeq            uint32
	NOutputsMax       uint32
	NOutputsMaxPerSeq uint32
	NThreads          int32
	NThreadsBatch     int32
	CtxType           int32
	RopeScalingType   int32
	PoolingType       int32
	AttentionType     int32
	FlashAttnType     int32
	RopeFreqBase      float32
	RopeFreqScale     float32
	YarnExtFactor     float32
	YarnAttnFactor    float32
	YarnBetaFast      float32
	YarnBetaSlow      float32
	YarnOrigCtx       uint32
	DefragThold       float32
	CbEval            unsafe.Pointer
	CbEvalUserData    unsafe.Pointer
	TypeK             int32
	TypeV             int32
	MoeCacheSize      uintptr
	AbortCallback     unsafe.Pointer
	AbortCallbackData unsafe.Pointer
	Embeddings        bool
	OffloadKqv        bool
	NoPerf            bool
	OpOffload         bool
	SwaFull           bool
	KvUnified         bool
	Samplers          unsafe.Pointer
	NSamplers         uintptr
	CtxOther          unsafe.Pointer
}

type llamaBatch struct {
	NTokens int32
	Token   *int32
	Embd    *float32
	Pos     *int32
	NSeqID  *int32
	SeqID   **int32
	Logits  *int8
}

type llamaFuncs struct {
	backendInit      func()
	backendFree      func()
	loadAllFromPath  func(dir string)
	logSet           func(cb uintptr, ud uintptr)
	modelDefault     func() llamaModelParams
	ctxDefault       func() llamaContextParams
	modelLoad        func(path string, p llamaModelParams) unsafe.Pointer
	modelFree        func(m unsafe.Pointer)
	initFromModel    func(m unsafe.Pointer, p llamaContextParams) unsafe.Pointer
	free             func(c unsafe.Pointer)
	modelVocab       func(m unsafe.Pointer) unsafe.Pointer
	modelNEmbd       func(m unsafe.Pointer) int32
	hasEncoder       func(m unsafe.Pointer) bool
	hasDecoder       func(m unsafe.Pointer) bool
	tokenize         func(v unsafe.Pointer, text *byte, n int32, toks *int32, max int32, special, parse bool) int32
	batchInit        func(n, embd, seqMax int32) llamaBatch
	batchFree        func(b llamaBatch)
	encode           func(c unsafe.Pointer, b llamaBatch) int32
	decode           func(c unsafe.Pointer, b llamaBatch) int32
	getMemory        func(c unsafe.Pointer) unsafe.Pointer
	memoryClear      func(m unsafe.Pointer, data bool)
	getEmbeddingsSeq func(c unsafe.Pointer, seq int32) unsafe.Pointer
	getEmbeddingsIth func(c unsafe.Pointer, i int32) unsafe.Pointer
	poolingType      func(c unsafe.Pointer) int32
	nCtxSeq          func(c unsafe.Pointer) uint32
}

var (
	loadOnce sync.Once
	loadErr  error
	fns      llamaFuncs
	libDirOf string
)

const maxSeqs = 8

func unavailable(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrUnavailable, fmt.Sprintf(format, a...))
}

// loadLib dlopens the native dependencies, then the llama.cpp libraries, by
// absolute path and binds the functions. It runs once per process.
func loadLib(libDir string) error {
	loadOnce.Do(func() {
		libDirOf = libDir
		loadErr = doLoad(libDir)
	})
	if loadErr == nil && libDirOf != libDir {
		return unavailable("llama.cpp already loaded from %s", libDirOf)
	}
	return loadErr
}

func doLoad(libDir string) error {
	dirs := libSearchDirs()
	for _, dep := range depLibs {
		if p := findLib(dirs, dep); p != "" {
			// Best effort: the system may provide it anyway.
			_, _ = purego.Dlopen(p, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		}
	}
	var handles []uintptr
	for _, name := range llamaLibs {
		p := filepath.Join(libDir, name)
		if _, err := os.Stat(p); err != nil {
			return unavailable("%s not found (run tix vault model fetch on the host)", p)
		}
		h, err := purego.Dlopen(p, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return unavailable("loading %s: %v", p, err)
		}
		handles = append(handles, h)
	}
	ggml, llama := handles[len(handles)-2], handles[len(handles)-1]

	var err error
	reg := func(fn any, h uintptr, name string) {
		if err != nil {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				err = unavailable("binding %s: %v", name, r)
			}
		}()
		purego.RegisterLibFunc(fn, h, name)
	}
	reg(&fns.loadAllFromPath, ggml, "ggml_backend_load_all_from_path")
	reg(&fns.backendInit, llama, "llama_backend_init")
	reg(&fns.backendFree, llama, "llama_backend_free")
	reg(&fns.logSet, llama, "llama_log_set")
	reg(&fns.modelDefault, llama, "llama_model_default_params")
	reg(&fns.ctxDefault, llama, "llama_context_default_params")
	reg(&fns.modelLoad, llama, "llama_model_load_from_file")
	reg(&fns.modelFree, llama, "llama_model_free")
	reg(&fns.initFromModel, llama, "llama_init_from_model")
	reg(&fns.free, llama, "llama_free")
	reg(&fns.modelVocab, llama, "llama_model_get_vocab")
	reg(&fns.modelNEmbd, llama, "llama_model_n_embd")
	reg(&fns.hasEncoder, llama, "llama_model_has_encoder")
	reg(&fns.hasDecoder, llama, "llama_model_has_decoder")
	reg(&fns.tokenize, llama, "llama_tokenize")
	reg(&fns.batchInit, llama, "llama_batch_init")
	reg(&fns.batchFree, llama, "llama_batch_free")
	reg(&fns.encode, llama, "llama_encode")
	reg(&fns.decode, llama, "llama_decode")
	reg(&fns.getMemory, llama, "llama_get_memory")
	reg(&fns.memoryClear, llama, "llama_memory_clear")
	reg(&fns.getEmbeddingsSeq, llama, "llama_get_embeddings_seq")
	reg(&fns.getEmbeddingsIth, llama, "llama_get_embeddings_ith")
	reg(&fns.poolingType, llama, "llama_pooling_type")
	reg(&fns.nCtxSeq, llama, "llama_n_ctx_seq")
	if err != nil {
		return err
	}

	// Silence llama.cpp and ggml logging (the callback is a no-op).
	fns.logSet(purego.NewCallback(func(level int32, text, ud uintptr) {}), 0)
	fns.loadAllFromPath(libDir)
	fns.backendInit()
	return nil
}

type llama struct {
	id, prefix string
	dims       int
	ctxLen     int
	model, ctx unsafe.Pointer
	vocab      unsafe.Pointer
	encoder    bool
	batch      llamaBatch
	batchCap   int
	mu         sync.Mutex
	closed     bool
}

// checkLayout compares llama_context_default_params() with the b11871
// defaults: a header change that moves fields would show up here instead of
// as silently wrong params or memory corruption.
func checkLayout(cp llamaContextParams) error {
	if cp.NCtx != 512 || cp.NBatch != 2048 || cp.NUbatch != 512 || cp.NSeqMax != 1 || cp.Embeddings {
		return unavailable("llama.h layout mismatch")
	}
	return nil
}

// OpenLlama loads the llama.cpp libraries from libDir and the GGUF model at
// modelPath. It returns an error wrapping ErrUnavailable when either is
// missing or can't be loaded.
func OpenLlama(libDir, modelPath string, o LlamaOptions) (Embedder, error) {
	if _, err := os.Stat(modelPath); err != nil {
		return nil, unavailable("model %s not found (run tix vault model fetch on the host)", modelPath)
	}
	if err := loadLib(libDir); err != nil {
		return nil, err
	}
	if o.Ctx <= 0 {
		o.Ctx = 512
	}
	if o.Threads <= 0 {
		o.Threads = min(runtime.NumCPU(), 4)
	}
	mp := fns.modelDefault()
	cp := fns.ctxDefault()
	if err := checkLayout(cp); err != nil {
		return nil, err
	}
	mp.NGPULayers = 0
	model := fns.modelLoad(modelPath, mp)
	if model == nil {
		return nil, unavailable("loading model %s failed", modelPath)
	}
	nBatch := o.Ctx * maxSeqs
	cp.NCtx = uint32(nBatch)
	cp.NBatch = uint32(nBatch)
	cp.NUbatch = uint32(nBatch)
	cp.NSeqMax = maxSeqs
	cp.NThreads = int32(o.Threads)
	cp.NThreadsBatch = int32(o.Threads)
	cp.Embeddings = true
	cp.PoolingType = -1 // the model's own
	cp.OffloadKqv = false
	cp.OpOffload = false
	ctx := fns.initFromModel(model, cp)
	if ctx == nil {
		fns.modelFree(model)
		return nil, unavailable("creating a context for %s failed", modelPath)
	}
	if fns.poolingType(ctx) == 0 {
		fns.free(ctx)
		fns.modelFree(model)
		return nil, unavailable("%s has no pooling type; not an embedding model", modelPath)
	}
	l := &llama{
		id: o.ID, prefix: o.QueryPrefix,
		dims:   int(fns.modelNEmbd(model)),
		ctxLen: o.Ctx,
		model:  model, ctx: ctx,
		vocab:    fns.modelVocab(model),
		encoder:  fns.hasEncoder(model) && !fns.hasDecoder(model),
		batch:    fns.batchInit(int32(nBatch), 0, 1),
		batchCap: nBatch,
	}
	return l, nil
}

func (l *llama) ID() string { return l.id }
func (l *llama) Dims() int  { return l.dims }

func (l *llama) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	fns.batchFree(l.batch)
	fns.free(l.ctx)
	fns.modelFree(l.model)
	return nil
}

func (l *llama) tokenize(text string) ([]int32, error) {
	b := []byte(text)
	var p *byte
	if len(b) > 0 {
		p = &b[0]
	}
	toks := make([]int32, len(b)+8)
	n := fns.tokenize(l.vocab, p, int32(len(b)), &toks[0], int32(len(toks)), true, false)
	if n < 0 {
		if n == math.MinInt32 {
			return nil, fmt.Errorf("tokenizing: text too large")
		}
		toks = make([]int32, -n)
		n = fns.tokenize(l.vocab, p, int32(len(b)), &toks[0], int32(len(toks)), true, false)
		if n < 0 {
			return nil, fmt.Errorf("tokenizing failed (%d)", n)
		}
	}
	toks = toks[:n]
	if len(toks) > l.ctxLen {
		// Keep the head and the closing special token (e.g. [SEP]).
		last := toks[len(toks)-1]
		toks = append(toks[:l.ctxLen-1], last)
	}
	return toks, nil
}

// Embed embeds texts, several per decode call.
func (l *llama) Embed(texts []string, query bool) ([][]float32, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, fmt.Errorf("embedder closed")
	}
	toks := make([][]int32, len(texts))
	for i, t := range texts {
		if query {
			t = l.prefix + t
		}
		var err error
		if toks[i], err = l.tokenize(t); err != nil {
			return nil, err
		}
		if len(toks[i]) == 0 {
			toks[i] = []int32{0}
		}
	}
	out := make([][]float32, len(texts))
	for start := 0; start < len(texts); {
		end, total := start, 0
		for end < len(texts) && end-start < maxSeqs && total+len(toks[end]) <= l.batchCap {
			total += len(toks[end])
			end++
		}
		if err := l.run(toks[start:end], out[start:end]); err != nil {
			return nil, err
		}
		start = end
	}
	return out, nil
}

// run decodes one group of sequences (at most maxSeqs, within the batch).
func (l *llama) run(group [][]int32, out [][]float32) error {
	b := l.batch
	n := 0
	tokens := unsafe.Slice(b.Token, l.batchCap)
	pos := unsafe.Slice(b.Pos, l.batchCap)
	nseq := unsafe.Slice(b.NSeqID, l.batchCap)
	seqs := unsafe.Slice(b.SeqID, l.batchCap)
	logits := unsafe.Slice(b.Logits, l.batchCap)
	for s, tk := range group {
		for p, t := range tk {
			tokens[n] = t
			pos[n] = int32(p)
			nseq[n] = 1
			*seqs[n] = int32(s)
			logits[n] = 1
			n++
		}
	}
	b.NTokens = int32(n)
	if mem := fns.getMemory(l.ctx); mem != nil {
		fns.memoryClear(mem, true)
	}
	var rc int32
	if l.encoder {
		rc = fns.encode(l.ctx, b)
	} else {
		rc = fns.decode(l.ctx, b)
	}
	if rc != 0 {
		return fmt.Errorf("llama decode failed (%d)", rc)
	}
	for s := range group {
		p := fns.getEmbeddingsSeq(l.ctx, int32(s))
		if p == nil {
			return fmt.Errorf("llama returned no embedding for sequence %d", s)
		}
		v := make([]float32, l.dims)
		copy(v, unsafe.Slice((*float32)(p), l.dims))
		normalize(v)
		out[s] = v
	}
	return nil
}

package embed

// Preloaded by absolute path (when found in the lib path) before dlopen.
var depLibs = []string{"libgomp.so.1", "libstdc++.so.6"}

// Loaded in this order from the lib dir; the last two hold the ggml and llama
// symbols.
var llamaLibs = []string{"libggml-base.so.0", "libggml.so.0", "libllama.so.0"}

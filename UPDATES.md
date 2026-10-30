# Components updated outside `nix flake update`

`nix flake update` only moves what's in `flake.lock`. Everything below is
pinned or fetched separately and has to be checked and bumped by hand. Keep
this list current whenever you add a pin, image or download.

| Component | Pinned | Where | How to update |
|---|---|---|---|
| Go dependencies | `go.sum` (cobra, yaml.v3, x/sys, x/term, purego, testscript) | `nix/tools.nix` (`vendorHash`) | `go get -u ./... && go mod tidy`, then build: set `vendorHash` to the hash the failed build reports. |
| llama.cpp library (local embeddings) | release b11871: the CPU builds for linux-amd64, linux-arm64, darwin-arm64 and darwin-amd64 (no Windows pin: `ct vault model` reports it unavailable), by sha256; the `struct` layouts in `internal/embed/llama.go` mirror its `llama.h` | `internal/modelcache/pins.json` (`llama`: version, per-platform URL, sha256, archive directory, and the libraries extracted) | Pick a release (`gh release view <tag> --repo ggml-org/llama.cpp --json assets`, the `digest` of each `llama-<tag>-bin-{ubuntu-x64,ubuntu-arm64,macos-arm64,macos-x64}.tar.gz` is its sha256), update version, URLs, sha256, `dir` and the `files` (archive member to installed soname; the CPU backends `libggml-cpu-*.so` change between releases). **Re-check `llamaModelParams`, `llamaContextParams` and `llamaBatch` against that release's `include/llama.h`** (purego passes them by value), then `ct vault model fetch` and `CT_EMBED_REAL=1 go test ./internal/embed -run Real -v`. A new version is a new cache dir, so the old one can be deleted by hand. |
| bge-small-en-v1.5 embedding model | Q8_0 GGUF from CompendiumLabs/bge-small-en-v1.5-gguf at commit d32f8c04, by sha256 | `internal/modelcache/pins.json` (`models`: file, URL, sha256, dims, query prefix) | New model or revision: set the URL (pin the commit, not `main`), `sha256sum` the file (Hugging Face shows it as `x-linked-etag`), dims and the query prefix its card recommends. A different model id or sha256 makes `ct vault index` re-embed everything. |
| Obsidian Tasks plugin | 8.4.0 | `assets/obsidian/tasks-plugin.json` (`version` + hex `sha256` per release asset): one pin, embedded in `ct` (`ct vault init`) and read by `nix/obsidian.nix` (`builtins.fromJSON`) | Set `version`, then the hash of each asset: `for f in main.js manifest.json styles.css; do printf '%s ' $f; curl -sL https://github.com/obsidian-tasks-group/obsidian-tasks/releases/download/<v>/$f \| sha256sum; done` (or `nix-prefetch-url <url>`, then `nix hash convert --hash-algo sha256 --to base16 <hash>`). If the release adds or drops an asset, edit the `sha256` map (Nix and `ct` install whatever it lists; `main.js` and `manifest.json` are required). Check `manifest.json`'s `minAppVersion` against the Obsidian in use. **Both `ct` and Nix seed it only into vaults that don't have it yet**, so a bump only reaches new vaults; existing vaults update through Obsidian. `go test ./internal/tasksplugin` checks the file. |
| Obsidian Minimal theme | 9.0.2 | `nix/obsidian.nix` (`minimalVersion`, 2× sha256) | `nix-prefetch-url https://github.com/kepano/obsidian-minimal/releases/download/<v>/{theme.css,manifest.json}`. **Held back:** 9.1.x needs Obsidian ≥ 1.14; check the release's `manifest.json` `minAppVersion` against `flatpak info md.obsidian.Obsidian`. Same seed-once caveat as Tasks. |

## Removed

| Component | Removed | Why |
|---|---|---|
| graphify (the code graph: `ct graph`, its image, the Python lock and base image) | 2026-10 | One A/B run over 10 questions showed no accuracy gain, ~6% higher cost, and ~1 min of startup per session. Sessions search with Claude Code's Grep, Glob and Read. |
| fff (the `mcp__fff__*` search tools the skills and agents named) | 2026-10 | Two A/B rounds made zero fff calls in 20 runs, and 14 days of transcripts show 16 fff calls against ~55k Bash calls. |

## Quick check

```sh
for r in obsidian-tasks-group/obsidian-tasks kepano/obsidian-minimal; do
  curl -sL -o /dev/null -w "$r %{url_effective}\n" "https://github.com/$r/releases/latest"
done
flatpak remote-info flathub md.obsidian.Obsidian | grep Version   # gate for Minimal 9.1+
```

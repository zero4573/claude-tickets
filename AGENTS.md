# AGENTS.md

Instructions for AI coding agents working on this repository (not the
vault's rulebook: that's `assets/vault-scaffold/AGENTS.md`, copied into each
vault).

## What this is

claude-tickets: a ticket workflow for Claude Code with an Obsidian vault as
long-term memory. One Go command, `ct` (`cmd/ct` + `internal/`), a single
self-contained binary: `assets/` (the vault scaffold, the Claude Code plugin
in `assets/plugin/`, the graphify image in `assets/graph/`) is embedded in
it, and the plugin and graph sources are unpacked to the cache on first use
(`assets.Materialize`). Nix packaging in `nix/` + `flake.nix`;
`configure` + `Makefile` for systems without Nix; CI in `.github/workflows/`
(manual-only until the first release). `README.md` is the operator's
guide; `UPDATES.md` lists every pin `nix flake update` doesn't move.

## Conventions

- **Sessions are plain Claude Code.** Commands start sessions only through
  `internal/session` (`session.Command`) with standard flags
  (`--plugin-dir`, `--add-dir`, `--mcp-config`, `--settings`) and the
  workspace's `.claude/settings.json`. Never add flags or knowledge of a
  particular sandbox or proxy: anything environment-specific is an
  optional `CLAUDE_TICKETS_*` variable with a sensible default, documented
  in the README's Configuration table and the home-manager module
  (`nix/home.nix`).
- **No workflow opinions in the defaults.** Only what the ticket system
  needs to work goes in `session.WriteSettings` (today: main clones and
  the graph cache are never edited). Everything else (git rules, MCP cost
  guards) is the user's, through `CLAUDE_TICKETS_SESSION_SETTINGS`, and
  recommended in the README.
- **Plugin names are namespaced:** skills are `/tickets:<skill>`, agents
  `tickets:<agent>`. Keep every reference (skills, tools, scaffold, README)
  in that form. The plugin's hooks run `ct hook ...`; `assets.Materialize`
  points them at this `ct` by path (the Nix package at its store `ct`).
- **One command, `ct`.** Subcommands live in `internal/cli` (one file per
  group), their logic in `internal/<area>`. Every change comes with tests:
  testscript CLI tests in `testdata/script/*.txtar` (stub `tmux`,
  `claude`, `podman`, `notify-send` and editor commands, real git), unit
  tests next to the code. User-facing names are always `ct …`.
- **ct sync is pinned by golden vaults:** `testdata/sync/` holds a fake
  Jira (`state<n>.json`, built by `fixtures.jq`, served by
  `internal/mcp/mcptest`) and the vaults three runs must leave
  (`golden/run<n>`; they started as the bash ticket-sync's output). A
  change to the notes ct sync writes updates them on purpose:
  `UPDATE_GOLDEN=1 go test -run TestScripts/sync .`, then review the diff.
  The `ticket-sync` skill writes the same layout for other sources: change
  both together.
- **Go:** keep file formats stable (notes, `.sources.json`, `.scaffold.json`,
  `.workflow.json`, `workspace.json`, `.sync-state.json`); helpers in
  `internal/<area>`; no new dependencies without a good reason (today: cobra, yaml.v3, x/sys, x/term, testscript).
- **Cross-platform (Linux, macOS, Windows; amd64 and arm64):** OS
  differences live in one place each:
  - directories and desktop tools (notify, open, shell): `internal/platform`.
    A pure function of GOOS (`DirsFor`) for anything testable, build tags
    otherwise.
  - ticket windows: `internal/launcher` (tmux, and none = foreground
    in this terminal; a new backend implements `Launcher`, declares its
    `Caps` and gets one entry in `registry.go`).
  - process liveness (PID + start time): `internal/proc`.
  - containers: `internal/container` (`Runtime`, `RunArgs`, `Mount`/`Path`).
  - locks, exec and writability: build-tagged files (`internal/lock`,
    `internal/gitx`, `exec_*.go`).

  Never compare or split filesystem paths with `"/"`: use `filepath` and
  `internal/fsx` (`Within`, `Inside`, `Depth`); `<provider>/<owner>/<repo>`
  IDs and wikilinks are `/`-separated strings, converted at
  `filepath.Join`. A new external tool comes with a seam and a fallback
  for where it doesn't exist. Help texts and messages show real paths
  (`config.TildePath`), not Linux ones. `GOOS=windows go vet ./...` and
  `make cross` must stay clean.
- **Keep examples neutral:** no real company, product, repository, ticket
  key or person names; use `acme`, `PROJ-12`, `Jane Doe`.
- **The vault scaffold has a history:** a change to `assets/vault-scaffold/`
  needs `go generate ./assets`, which adds the new versions' hashes to
  `assets/scaffold-history.json` (read from git, so it needs full, not
  shallow, history; hashes are only ever added). `ct vault update` uses it
  to tell an unedited old copy in a vault from an edited one, and
  `TestHistoryCoversScaffold` fails until it's run. On a merge conflict in
  the file, take either side and run it again.
- **Pins** (images by digest, the hashed graphify lock, fetched Obsidian
  assets) get a row in `UPDATES.md`.

## Checking

```sh
go generate ./assets          # after changing assets/vault-scaffold/: its hash history
make check                    # go vet, gofmt, go test (unit + CLI tests, golden ct sync runs)
make cross                    # every OS/arch into dist/
nix build .#default           # builds ct (runs go test)
nix flake check
./configure --prefix="$(mktemp -d)" && make install   # the non-Nix path
```

The CLI tests use `sh` stubs and skip on Windows; anything OS-specific
gets a unit test that runs everywhere (e.g. `platform.DirsFor` for every
GOOS).

Test commands in a throwaway `HOME` / `XDG_CONFIG_HOME` / `OBSIDIAN_ROOT`
with a stub `claude` on `PATH` that records its arguments; never against a
real vault.

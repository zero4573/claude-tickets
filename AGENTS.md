# AGENTS.md

Instructions for AI coding agents working on this repository (not the
vault's rulebook: that's `assets/vault-scaffold/AGENTS.md`, copied into each
vault).

## What this is

claude-tickets: a ticket workflow for Claude Code with an Obsidian vault as
long-term memory. One Go command, `ct` (`cmd/ct` + `internal/`, with the
vault scaffold embedded from `assets/`), a Claude Code plugin in `plugin/`,
the graphify image in `graph/`, Nix packaging in `nix/` + `flake.nix`, and
`install.sh` for systems without Nix. `README.md` is the
operator's guide; `UPDATES.md` lists every pin `nix flake update` doesn't
move.

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
  in that form. The plugin's hooks run `ct hook ...` (the packages point
  them at an absolute `ct`).
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
- **Go:** keep file formats stable (notes, `.sources.json`,
  `.workflow.json`, `workspace.json`, `.sync-state.json`); helpers in
  `internal/<area>`; no new dependencies without a good reason (today: cobra, yaml.v3, x/sys, x/term, testscript).
- **Portable:** Go code builds for Linux, macOS and (later) Windows: OS
  specifics behind build tags (`internal/lock`, `internal/gitx`,
  `exec_*.go`); podman or docker only through `internal/container`.
- **Keep examples neutral:** no real company, product, repository, ticket
  key or person names; use `acme`, `PROJ-12`, `Jane Doe`.
- **Pins** (images by digest, the hashed graphify lock, fetched Obsidian
  assets) get a row in `UPDATES.md`.

## Checking

```sh
go test ./...                 # unit + CLI tests, golden ct sync runs
nix build .#default          # builds ct (runs go test)
nix flake check
./install.sh --prefix "$(mktemp -d)"   # the non-Nix path
```

Test commands in a throwaway `HOME` / `XDG_CONFIG_HOME` / `OBSIDIAN_ROOT`
with a stub `claude` on `PATH` that records its arguments; never against a
real vault.

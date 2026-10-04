# AGENTS.md

Instructions for AI coding agents working on this repository (not the
vault's rulebook: that's `tools/vault-scaffold/AGENTS.md`, copied into each
vault).

## What this is

claude-tickets: a ticket workflow for Claude Code with an Obsidian vault as
long-term memory. Bash commands in `tools/`, a Claude Code plugin in
`plugin/`, the graphify image in `graph/`, Nix packaging in `nix/` +
`flake.nix`, and `install.sh` for systems without Nix. `README.md` is the
operator's guide; `UPDATES.md` lists every pin `nix flake update` doesn't
move.

## Conventions

- **Sessions are plain Claude Code.** Commands start sessions only through
  `claude_session_cmd` (tickets-lib.sh) with standard flags
  (`--plugin-dir`, `--add-dir`, `--mcp-config`, `--settings`) and the
  workspace's `.claude/settings.json`. Never add flags or knowledge of a
  particular sandbox or proxy: anything environment-specific is an
  optional `CLAUDE_TICKETS_*` variable with a sensible default, documented
  in the README's Configuration table and the home-manager module
  (`nix/home.nix`).
- **No workflow opinions in the defaults.** Only what the ticket system
  needs to work goes in `write_session_settings` (today: main clones and
  the graph cache are never edited). Everything else (git rules, MCP cost
  guards) is the user's, through `CLAUDE_TICKETS_SESSION_SETTINGS`, and
  recommended in the README.
- **Plugin names are namespaced:** skills are `/tickets:<skill>`, agents
  `tickets:<agent>`. Keep every reference (skills, tools, scaffold, README)
  in that form.
- **Each command is `tools/<name>.sh` with `tools/tickets-lib.sh`
  prepended**, built by `nix/tools.nix` (writeShellApplication runs
  shellcheck, so the build is the lint) and by `install.sh` the same way. A
  new command needs an entry in both, and in `sessionTools` if sessions run
  it. Shared helpers go in tickets-lib.sh with
  `# shellcheck disable=SC2329 # shared helper; not every script uses it`.
- **Portable bash:** bash 4+, GNU tools (tickets-lib.sh maps the
  g-prefixed ones on macOS), podman or docker through `container_run` /
  `container_runtime` only.
- **Keep examples neutral:** no real company, product, repository, ticket
  key or person names; use `acme`, `PROJ-12`, `Jane Doe`.
- **Pins** (images by digest, the hashed graphify lock, fetched Obsidian
  assets) get a row in `UPDATES.md`.

## Checking

```sh
nix build .#default          # builds every command (shellcheck included)
nix flake check
./install.sh --prefix "$(mktemp -d)"   # the non-Nix path
```

Test commands in a throwaway `HOME` / `XDG_CONFIG_HOME` / `OBSIDIAN_ROOT`
with a stub `claude` on `PATH` that records its arguments; never against a
real vault.

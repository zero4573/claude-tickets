# Architecture

claude-tickets is one Go binary, `ct`, plus an Obsidian vault the agents use as long-term
memory. `ct` prepares workspaces and launches Claude Code sessions; the sessions do the
work, following the plugin's skills. Everything the plugin, the vault scaffold and the
graph image need is embedded in `ct` (`assets/`) and unpacked to the cache on first use.

```mermaid
flowchart TB
  user(["you"]) --> ct["ct (cmd/ct, internal/cli)"]
  ct --> sync["sync: internal/jira, internal/followup"]
  ct --> ws["workspaces: internal/workspace, internal/gitx, internal/repo"]
  ct --> sess["sessions: internal/launcher, internal/session, internal/prompt"]
  ct --> vaultcmd["vault tools: internal/vault, internal/links, internal/lock, internal/note"]
  ct --> search["search: internal/vsearch, internal/vindex, internal/embed, internal/chunk"]
  ct --> graph["code graph: internal/graph, internal/container, assets/graph"]
  sync --> vault[("Obsidian vault")]
  vaultcmd --> vault
  search --> vault
  ws --> work[("workRoot/ID: worktrees, workspace.json, .tool-versions")]
  sess --> claude["claude (plugin tickets, MCP servers, hooks)"]
  claude --> work
  claude --> vault
  claude --> graph
  claude -- "status hooks" --> status["ct status, desktop notifications"]
```

## Packages

| Package | Role |
|---|---|
| `internal/cli` | the cobra commands: `start`, `status`, `ws`, `vault`, `kb`, `teach`, `clean`, `sync`, `graph`, `layout`, `claude`, `hook` |
| `internal/workspace` | a ticket workspace: `workspace.json` (repos, bases, branches, target versions), the docs and tidy marks, the live sessions, the `.tool-versions` stamp |
| `internal/gitx`, `internal/repo` | git helpers (worktrees, unpushed commits) and the repo slug rules |
| `internal/vault`, `internal/config` | finding the current vault, its `.workflow.json`, tool versions |
| `internal/links`, `internal/lock`, `internal/vaultlock` | moving notes without breaking wikilinks, and the vault lock `/tickets:save` takes |
| `internal/vsearch`, `internal/vindex`, `internal/embed`, `internal/chunk`, `internal/modelcache` | the vault search index and its embedding model |
| `internal/jira`, `internal/followup` | ticket sources and the follow-up notes `ct` leaves |
| `internal/launcher`, `internal/session`, `internal/watch` | running sessions in the foreground of this terminal, and the session state behind `ct status` |
| `internal/graph`, `internal/container` | the code graph image (podman or docker) and the stdio MCP server |
| `internal/scaffold`, `assets/` | the vault scaffold and its hash history, the plugin, the graph sources |

## The plugin

`assets/plugin/` is the Claude Code plugin `tickets`. Its skills are the playbooks: the PO
(`work-ticket`), the lead, the task agent, the reviewer, and the closing steps `docs`,
`tidy` and `save`. Its agents (`lead`, `task`, `mechanical`, `reviewer`, `reviewer-deep`,
`analyst`) and hooks are what `ct` passes to `claude` with `--plugin-dir`. The flow is in
[the ticket workflow](ticket-workflow.md).

## Code graph

Each session's code graph merges the ticket worktrees' graphs with every other repo's main
clone. A worktree's graph replaces its main clone's, and node ids are prefixed with the repo
slug. `ct start` runs the merge before a session and `ct graph merge <workspace>` runs it
on demand; in a dev workspace the session's graph MCP is served by the built `ct` (see
[Workspaces and the vault](workspace-and-vault.md)).

```mermaid
flowchart LR
  main[("main clones: graphify-out")] -- "seed at ws add, and after ws gc" --> wg[("workRoot/ID/.cache/graphify/slug/graphify-out")]
  wt["ticket worktree"] -- "graphify watch, GRAPHIFY_OUT" --> wg
  wg --> merge["ticket-merge.py: node ids prefixed with slug"]
  main --> merge
  merge --> out[("workRoot/ID/graphify-out/graph.json")]
  out --> serve["graphify MCP server, hot reload"]
```

Details of where worktree graphs live are in
[workspaces and the vault](workspace-and-vault.md#worktree-code-graphs).

## Layout on disk

- The vault: `tickets/<source>/<ID>/` (note, plan, board, relay, tasks, contracts, reviews),
  `projects/<slug>/`, `knowledge-base/`, `references/`, `inbox/`, `learning/`.
- The workspace: `<workRoot>/<ID>/` with `CLAUDE.md`, `workspace.json`, `.tool-versions`,
  one worktree per repo at `<ID>/<slug>`, task worktrees under `tasks/`, and `.cache/` (Go
  modules and the worktree graphs).
- Main clones: `<projectsRoot>/<provider>/<owner>/<repo>`; never edited, only their `.git`
  is written so worktrees can be created.

See [workspaces and the vault](workspace-and-vault.md) for the details, and
[finishing a ticket](finishing-a-ticket.md) for the order of the closing steps.

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
| `internal/gitx`, `internal/repo` | git helpers (worktrees, unpushed commits, signing) and the repo slug rules |
| `internal/vault`, `internal/config` | finding the current vault, its `.workflow.json`, tool versions |
| `internal/links`, `internal/lock`, `internal/vaultlock` | moving notes without breaking wikilinks, and the vault lock `/tickets:save` takes |
| `internal/vsearch`, `internal/vindex`, `internal/embed`, `internal/chunk`, `internal/modelcache` | the vault search index and its embedding model |
| `internal/jira`, `internal/followup` | ticket sources and the follow-up notes `ct` leaves |
| `internal/launcher`, `internal/session`, `internal/watch` | tmux or terminal launching and the session state behind `ct status` |
| `internal/graph`, `internal/container` | the code graph image (podman or docker) and the stdio MCP server |
| `internal/scaffold`, `assets/` | the vault scaffold and its hash history, the plugin, the graph sources |

## The plugin

`assets/plugin/` is the Claude Code plugin `tickets`. Its skills are the playbooks: the PO
(`work-ticket`), the lead, the task agent, the reviewer, and the closing steps `docs`,
`tidy` and `save`. Its agents (`lead`, `task`, `mechanical`, `reviewer`, `reviewer-deep`,
`analyst`) and hooks are what `ct` passes to `claude` with `--plugin-dir`. The flow is in
[the ticket workflow](ticket-workflow.md).

## Layout on disk

- The vault: `tickets/<ID>/` (note, plan, board, relay, tasks, contracts, reviews),
  `projects/<slug>/`, `knowledge-base/`, `references/`, `inbox/`, `learning/`.
- The workspace: `<workRoot>/<ID>/` with `CLAUDE.md`, `workspace.json`, `.tool-versions`,
  one worktree per repo at `<ID>/<slug>` and task worktrees under `tasks/`.
- Main clones: `<projectsRoot>/<provider>/<owner>/<repo>`; never edited, only their `.git`
  is written so worktrees can be created.

See [workspaces and the vault](workspace-and-vault.md) for the details, and
[finishing a ticket](finishing-a-ticket.md) for the order of the closing steps.

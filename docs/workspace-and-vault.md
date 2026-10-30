# Workspaces and the vault

## The vault

The vault is an Obsidian folder `ct vault init` scaffolds from `assets/vault-scaffold/`
(its `AGENTS.md` is the rulebook agents follow inside it). `ct vault update` keeps shipped
files current using the hash history in `assets/scaffold-history.json`, so an unedited old
copy is replaced and an edited one is kept. Per-vault settings (`workRoot`,
`projectsRoot`, `toolVersions`) live in `.workflow.json`; ticket sources in
`tickets/.sources.json`. A ticket's folder is `tickets/<source>/<ID>/`, `<source>` being
its source's key there (`jira`, `manual`, ...) and its note's `source:`; `ct vault update`
moves the folders of an older vault (`tickets/<ID>/`) under their source.

```mermaid
flowchart LR
  v[("vault")] --> t["tickets/source/ID: note, plan, board, relay, tasks, contracts, reviews"]
  v --> p["projects/slug: index, features, decisions, logs"]
  v --> k["knowledge-base/ and references/"]
  v --> i["inbox/: drafts for save"]
  v --> l["learning/topic"]
  v --> c[".ct/: search index, rebuildable"]
```

Only `/tickets:save` writes the shared notes (`projects/`, `knowledge-base/`,
`references/`). Other sessions write their ticket folder and leave drafts in `kb-drafts/`
or `inbox/`.

## A ticket workspace

`ct ws add <ID> <repo> --base <branch> [--desc <d>] [--version <v>] [--branch <b>]`
creates the ticket's worktree of a repo at `<workRoot>/<ID>/<slug>` and records the repo,
base, branch and target version in `workspace.json`. The branch is
`feature/<ID>[-<description>]` for a synced ticket and `feature/<description>` for a manual
one. `--branch` checks out an existing branch instead, for example an open PR's source. A
manual ticket's branch that another workspace has already recorded is refused, so two
tickets never share a branch.

Each `ct start`, `ct ws add`, `ct spec` and `ct teach` writes `<workRoot>/<ID>/.tool-versions`
(golang from the repos' `go.mod`, python and others from `toolVersions` in
`.workflow.json`; a tool with no installed version gets a warning and is left out). The
workspace's `CLAUDE.md` is regenerated on every start and names the vault, the repos and
the Git rules.

Task worktrees live under `<ID>/tasks/<slug>-<n>`, are created and removed by the lead,
and are merged into the ticket branch. `ct ws rm` removes a workspace (refusing dirty or
unpushed work, and ignored files such as a changed `.env` unless `--drop-ignored`; commit, push or stash the rest: there is no force), `ct ws gc` and `ct clean` free disk, moving ignored files of yours to the vault's `.trash/<ID>-ignored-files/` instead of deleting them, `ct ws fetch` refreshes the main
clones. A session can clone a repo it needs into the projects root (`ct ws clone`, never
twice), then make its worktree with `ct ws add`; there are no other copies of a repo. A session can write the repo directory (`projectsRoot`): it may clone repos and add worktrees there. The sandbox keeps the `.git/hooks`, `.git/config` and git pointer files (`.git` files, `commondir`, `gitdir`) of clones that already exist read-only. ct's own git (`gitx`) first reads the repo's own config files in Go (`config`, `config.worktree`, and the submodules' under `modules/`) and refuses the repo when any key is off a safe list, so a planted `core.fsmonitor`, `core.sshCommand`, `core.hooksPath`, `core.pager`, an `include`, an external filter or diff driver, a `protocol` or `url.*.insteadOf` rewrite and the like never run; it refuses a symlinked or oversized config file and a worktree whose git pointer files lead outside `projectsRoot`/`workRoot` (a layout check, not proof: a session can write both folders). Only `https`, `ssh` and scp-like `user@host:path` URLs are allowed for remotes, and only `lfs` filters with git-lfs's own commands. On top of that git runs with hooks, fsmonitor, `core.gitProxy`, credential helpers (`credential.helper=` resets the list, so one from your own git config is off for ct too), signing, submodule recursion and every protocol but https and ssh off, `--no-ext-diff --no-textconv` on diffs and logs that read patches, `GIT_SSH_COMMAND` yours or plain `ssh`, and `GIT_DIR`, `GIT_WORK_TREE` and `GIT_CONFIG*` removed from the environment. `ct ws clone` clones with `--no-checkout`, then checks out in the new clone, which ct checks like any other. The allowlist reads your global and system config as trusted. Not covered: your own git in a clone made during a session, whose config the sandbox doesn't make read-only until the next launch (run it with `-c core.hooksPath=/dev/null` or read `.git/config` first); `.lfsconfig` in the work tree, which can redirect LFS URLs (network only, no program); the milliseconds between ct's check and git reading the file; a submodule's `core.worktree` pointing inside the work tree; a local shared-clone source that passes the check; and an origin written as a `host-alias:path` (refused outright, not rewritten). `ct ws refresh <ID>` rewrites the workspace's `CLAUDE.md` from the vault
exactly as `ct start` does and touches nothing else; the kickoff runs it once `board.md`
names the ticket's commit style, so the Git rules in `CLAUDE.md` are current. The tidy skill's scratch clones (`tasks/tidy-<slug>`) are removed with
`ct ws tidy-clean <ID>`.

### Dev workspaces

A ticket that changes claude-tickets itself would otherwise run its agents on the installed
plugin and `ct`, so its own new rules never apply while it is worked. `ct start <ID> --dev
<slug>` records `"dev": {"slug": "<slug>"}` in `workspace.json` (omitted when off; `--dev
none` clears it), where `<slug>` is a repo of the workspace with `cmd/ct` and
`assets/plugin`. Every start of a dev workspace then builds `ct` from that checkout into
`.cache/dev/bin` (the workspace's Go env; a failed build fails the start, nothing falls
back), copies the checkout's `assets/plugin` to `.cache/dev/plugin` with its hooks running
the built `ct` by path, and gives the sessions that plugin, `.cache/dev/bin` prepended to
and the `PATH` they start with (expanded where `claude` starts, not a copy of `ct start`'s
own).
`ct start` prints `dev mode: plugin and ct from <slug> (<short HEAD>[-dirty])`. The
sessions also get `CLAUDE_TICKETS_DEV_BIN=<workspace>/.cache/dev/bin`: a sandbox or
container wrapping `claude` that sets its own `PATH` prepends it (the path is in the
workspace, so it exists inside too); otherwise skills and hooks run the installed `ct`.

The installed `ct` never runs the built binary or other checkout code outside a session:
it only builds (the go.mod toolchain and module downloads still run on the host) and copies
regular files and dirs (a symlink or other special file in `assets/plugin`, or
`assets/plugin` itself a link, fails the start with an error naming it), and the copy
refuses to replace a `.cache/dev/plugin` that isn't a plugin. A running
session keeps the build it started with: `ct start` refuses a ticket whose session runs, so
every start builds and copies afresh. `ct clean --save` ignores `dev`: its unattended `/tickets:save`
runs on the installed plugin and `ct`. An unknown `<slug>` fails with `<slug> is not a repo
of <ID>'s workspace (ct ws add first)`.

```mermaid
sequenceDiagram
  participant U as user
  participant S as installed ct start
  participant G as go build
  participant W as workspace .cache/dev
  participant C as claude session
  U->>S: ct start PROJ-12 --dev acme-ct
  S->>S: record dev slug in workspace.json
  S->>G: build ./cmd/ct from the checkout
  G-->>W: bin/ct
  S->>W: copy assets/plugin, regular files only, hooks run bin/ct
  S-->>U: dev mode: plugin and ct from acme-ct
  S->>C: becomes claude with --plugin-dir, PATH with bin first, CLAUDE_TICKETS_DEV_BIN
  C->>W: skills and hooks run the built ct
```

## Repo slugs

A repo is named `<provider>-<owner>-<repo>`, from its origin URL. The slug names the vault
project folder, the worktree folder and version tags. `ct layout` moves
main clones to `<projectsRoot>/<provider>/<owner>/<repo>`.

## Cleanup

`ct clean` removes a closed ticket's workspace once its work is saved and pushed, then
moves a synced ticket's folder to `.trash/` and a manual one's to `archive/tickets/<source>/<ID>/`.

# Obsidian Vault - Instructions for Agents

## What is this vault
Centralized knowledge base for all projects. Persistent memory across sessions.
It is also the tracking surface for tickets (from Jira, created by hand, or any
other source) worked by parallel, sandboxed agents (see **Ticket workflow**).

## Vault Structure
```
ROOT
├── .workflow.json         # where this vault's tools work: workspaces, main clones, tmux session (with tmux)
├── .scaffold.json         # what ct shipped here (ct vault update); don't edit
├── raw/                   # unorganized manually created docs/ideas (agents: ignore)
├── pending.md             # task views (Tasks plugin): open tasks across the vault (agents: ignore)
├── done.md                #   finished tasks (agents: ignore)
├── follow-ups.md          #   open #follow-up tasks: people to chase about blockers (agents: ignore)
├── tickets.base           # Bases dashboard: Active / Waiting on me / Blocked by dependencies / Leads / By lead / Manual / Ignored / Done / All
├── projects.base          # Bases view of projects/: Projects / Groups / Ungrouped
├── tickets/               # one folder per ticket, from any source
│   ├── .sources.json      #   ticket sources: jira, manual, ... (see "Ticket sources")
│   ├── .sync-state.json   #   last ct sync run, per source
│   └── PROJ-12/          #   one ticket, named by its ID
│       ├── PROJ-12.md    #     ticket note: content, workspace, tasks, questions, review, summaries
│       ├── requirements.md, design.md, investigation.md, dev-notes.md, qa-report.md
│       │                  #     role hand-off files (created as needed)
│       ├── pr-feedback.md #     PR review threads: outcome + draft reply each (/tickets:pr-feedback)
│       ├── kb-drafts/     #     knowledge notes drafted during the ticket; promoted by /tickets:save
│       └── logs/          #     ticket session logs
├── archive/               # left by ct clean (never edit): tickets/<ID>/ of archived manual tickets,
│                          #   and the files other notes still link from removed synced tickets
├── inbox/                 # knowledge drafts from non-ticket sessions (kb skill); promoted by /tickets:save
│                          #   also follow-up notes (type: follow-ups): tasks unattended commands leave for you
├── templates/             # templates to use for various note types
├── logs/                  # global session logs (not tied to a project)
├── references/            # reference material on specific behaviour outside any project/ticket
├── knowledge-base/        # general findings, in topic subfolders
├── projects/              # folder for all project notes (see "Projects and groups")
│   ├── <slug>/            #   one project per repo, named by the repo's slug (below); standalone by default
│   │   ├── <slug>.md      #     index note (template: project.md): depends-on, groups
│   │   ├── architecture/  #     architecture, conventions; <slug>-decisions.md (decision index)
│   │   ├── sequences/     #     data flows within the project (mermaid)
│   │   ├── data/          #     schema, data model
│   │   ├── features/      #     planned/implemented features (projects touched, versions)
│   │   └── logs/          #     project session logs
│   └── <group>/           #   (optional) a group of projects that work together
│       ├── <group>.md     #     index note (template: group.md, type: group): interactions
│       ├── architecture/  #     <group>-decisions.md, <group>-compatibility.md
│       ├── sequences/     #     data flows across its members (mermaid)
│       ├── features/      #     features across its members
│       └── logs/          #     group session logs
└── AGENTS.md              # global instructions for AI Agents
```

### Repo slugs
Every repo is named by its **slug**, `<provider>-<owner>-<repo>` in kebab-case,
taken from its git origin. Examples: `bitbucket-acme-billing-service`
(Bitbucket project SHIPS) and `github-ato-dotfiles` (GitHub user ato). The slug
names the project folder and its notes, the `projects:` and `versions:` entries
of tickets, version tags, worktree folders and the code graph's node ids. The
main clone lives at `~/Projects/<provider>/<owner>/<repo>`; `ct ws repos`
lists every slug. Project index notes carry `aliases: [<repo>]` so the short
name still finds them.

### Projects and groups
Every repo is a **project**, `projects/<slug>/`, and stands alone unless
its notes say otherwise. A vault may hold unrelated projects, one set of
projects that work together, or several such sets.
- **`depends-on`** (frontmatter of the project's index note,
  `["[[<other-slug>]]", …]`): the projects of this vault this one needs at
  run, build or deploy time (it calls their API, consumes their events,
  reads their database, packages or imports them). `A depends-on B` means
  a change to B can break A; a two-way interaction gets a link on each
  side. The `## Depends on` table in the body says how (interface or
  contract) and since which version; dependencies outside the vault go
  there as text. "Used by" isn't stored: backlinks show it.
- **`groups`** (`["[[<group>]]", …]`): the groups the project belongs to.
  A missing or empty `depends-on` or `groups` means none.
- **Groups** are optional: `projects/<group>/<group>.md` (template
  `group.md`, `type: group`) holds what several projects share: the
  interaction map (`## Interactions`, mermaid, kept in step with the
  members' `depends-on`), flows across members (`sequences/`),
  `<group>-compatibility` (feature, project, minimum version) and
  `<group>-decisions`. A group has no members list: its members are the
  projects whose `groups` link it. Groups are flat, and a project may be
  in several. A group's name is kebab-case, unique in the vault, says
  what the projects do together (`orders-platform`, `home-infra`), and is
  never a repo slug. Only `/tickets:save` creates one, and only after
  asking you.
- **Related or not:** projects and groups linked through `depends-on` or
  `groups`, directly or not, form a **cluster**: they're related.
  Projects in different clusters are unrelated; a project without links
  is standalone. Only notes with `type: project` or `type: group` count.
- **`ct vault groups <vault> [<slug|group>...]`** prints the clusters, or
  one project's groups, `depends-on`, used-by and cluster. A repo with
  no project note yet shows as `<slug>: no note (standalone)`. With
  `--json` it prints JSON; `--check` fails on a `groups` link that isn't
  a group, a `depends-on` link that isn't a project, or a project
  depending on itself (with names, only in those notes; without, anywhere
  in the vault). By hand:
  read the index note, then grep `projects/*/*.md` for `[[<slug>]]`.

## Note Rules

### Creation
- Use wikilinks: `[[note-name]]` (not markdown links)
- Link by **bare note name** (`[[order-sync-flow]]`), never by path
  (`[[projects/x/order-sync-flow]]`). Obsidian resolves bare names across the
  whole vault, so notes can move between folders without breaking links.
- Every **filename is unique across the vault** (bare-name links depend on
  it): projects are named by slug, groups by a name that is never a slug,
  decision indexes are `<slug>-decisions.md` (`<group>-decisions.md`),
  and session logs are `yyyy-MM-dd-<ID>-<slug>-<description>.md`.
- Mandatory YAML frontmatter on every note
- Filenames in kebab-case: `auth-flow.md`, not `Auth Flow.md`
- Tags in kebab-case: `billing-api`, not `Billing API` (no dots either: Obsidian tags can't contain them)
- 1 concept per permanent note (atomicity)
- Minimum 2 wikilinks per note (dense linking)
- Use a template if one exists, otherwise, use the base template as a starting point

Note: `{{date}}`, should be replaced with the date in `yyyy-MM-dd` format, and `{{title}}` should be replaced with the note title

### Templates
| Template | For |
|---|---|
| `base.md` | anything without a specific template |
| `ticket-synced.md` | tickets synced from a source (created by ct sync) |
| `ticket-manual.md` | tickets you write yourself (`ct new`, or insert it into `tickets/MAN-<n>/MAN-<n>.md`) |
| `session-log.md` | ticket and project session logs |
| `project.md` | `projects/<slug>/<slug>.md` index notes (`depends-on`, `groups`) |
| `group.md` | `projects/<group>/<group>.md`: a group of projects that work together |
| `feature.md` | `projects/<slug>/features/` or `projects/<group>/features/`: projects touched, versions |
| `sequence.md` | data flows (mermaid `sequenceDiagram`) |
| `decision.md` | decisions with their tradeoffs |
| `investigation.md` | bug traces and investigations |
| `knowledge.md` | `knowledge-base/<topic>/` |
| `reference.md` | `references/` |
| `pr-feedback.md` | `tickets/<ID>/pr-feedback.md`, written by `/tickets:pr-feedback` |

### Standard frontmatter
```
---
title: Note Name
created: yyyy-MM-dd
updated: yyyy-MM-dd
status: active
type: permanent
tags: [project, topic1, topic2, ...]
---
```

### Never do
- Don't delete notes without asking. `ct clean`, run by you, is the only way
  ticket notes leave the vault; nothing else deletes them
- Don't use markdown links for internal notes (use wikilinks)
- Don't create notes without frontmatter
- Don't change folder structure without documenting it
- Don't move notes with `mv`: use `ct vault links move` (keeps links working)

### Task lists
When creating task lists, use the following task list options to track work:
- `- [ ]` - TODO task
- `- [/]` - In Progress task
- `- [x]` - Completed task

Each of the above options should be followed by the task name, `➕ yyyy-MM-dd` when the task was created, and `✅ yyyy-MM-dd` when the task is completed.
In ticket notes, tag each task with the role that owns it (`#role/architect`).

examples:
- `- [ ] task name here ➕ yyyy-MM-dd` - TODO task
- `- [/] task name here #role/developer ➕ yyyy-MM-dd` - In Progress task
- `- [x] task name here ➕ yyyy-MM-dd ✅ yyyy-MM-dd` - Completed task

## Ticket workflow

The tools come from claude-tickets: the commands below, and a Claude Code
plugin (`tickets`) with the skills (`/tickets:<skill>`), the role agents
and the hooks. Sessions run your `claude` command (which may be sandboxed).

| Command | What it does |
|---|---|
| `ct vault default [<vault>\|--pick\|--unset]` | Show or set the default vault (`~/.config/claude-tickets/default-vault`): the one every ticket command acts on. Switch it to work on another vault; running sessions keep the vault they started with. `ct vault init` ignores it and always asks. |
| `ct claude [claude args]` | A plain repo session with the default vault as its knowledge base (see "Sessions outside tickets"). Opt-in: a plain `claude` session doesn't see the vault. |
| `ct kb [--print] ["<question>"]` | Ask about the projects (repos, how they work together, architecture, flows, past tickets) without a ticket: a session with this vault, every repo (fetched first) read-only, and one code graph of all of them. It can explore any branch, tag or commit in its own clones (`ct kb repo`), building and testing there, but never commits or pushes, and it files manual tickets (tag `from-kb`) for issues worth following up. It investigates unknowns and drafts what it learns into `inbox/`; `/tickets:save` promotes the drafts. |
| `ct vault init [<vault>]` | Set a vault up for this workflow: folders, this AGENTS.md, templates, the dashboard, Obsidian settings, then `ct vault configure` for the settings not made yet, and the Tasks community plugin (downloaded when missing, never replaced). Keeps existing files, so it's safe to re-run; `ct vault update` brings unedited ones up to date. |
| `ct vault update [<vault>] [--dry-run] [--diff] [--take <file>]` | Bring the files ct ships (this AGENTS.md, templates, the dashboard, task views) up to date: unedited ones are replaced, edited ones (and ones a newer ct wrote) kept and reported, nothing deleted or downgraded. |
| `ct vault configure [<vault>] [--section locations\|sources\|runtime]` | The vault's settings, each prompt showing its current value: locations (`.workflow.json`, see below), ticket sources (`tickets/.sources.json`), and the container runtime of the code graph (host-wide). |
| `ct sync [--source jira] [--full]` | Pull your open tickets from every syncable source into `tickets/` and reconcile the notes with the source (read-only, through each source's MCP server; for Jira see [[atlassian-rovo-mcp-setup]]). For Jira the command syncs by itself, without a model: Jira's changelog says what changed in each ticket, and only that part of the note is rewritten (frontmatter and table, description, comments, an epic's children); time tracking and other changes the note doesn't show only move the timestamp. Claude (the source's `model`, default sonnet) runs only for a description or comments Jira can give only as HTML. Problems land in an `inbox/` follow-up note. |
| `ct new [--type bug] "<summary>"` | Create a manual ticket `MAN-<n>` from `templates/ticket-manual.md`. |
| `ct start [--no-attach] <ID>...` | One tmux window per named ticket (with one ID it switches to it), in the tmux session `tickets-<vault>` (several at once are fine; Tab completes the open ones, `--list` prints them), each a Claude session running `/tickets:work-ticket <ID>` in its workspace `<workRoot>/<ID>`. Without a multiplexer (`CLAUDE_TICKETS_LAUNCHER=none`, or no tmux installed): one ticket, run in the current terminal. |
| `ct feedback <ID>...` | Same, but runs `/tickets:pr-feedback <ID>`: applies the review feedback on your open Bitbucket PRs for the ticket in its worktrees (committed, not pushed) and drafts a reply per thread in `tickets/<ID>/pr-feedback.md`. In an already-open ticket session, just type `/tickets:pr-feedback`. |
| `ct status` | Every ticket session: waiting on you (`needs-input`), `idle`, `working`, `exited`; source; vault status; dirty repos. |
| `ct attach <ID>` | Jump to a ticket's window. |
| `ct open <ID>` | Open the ticket's workspace in `$CLAUDE_TICKETS_EDITOR` (default `code`; VS Code gets a multi-root workspace of its worktrees and these notes). |
| `ct ws repos\|clone\|add\|ls\|diff\|rm\|fetch` | Git worktrees per ticket: `<workRoot>/<ID>/<slug>` on `feature/<ID>[-<description>]`. `clone` (run on the host) fetches a repo the kickoff found on Bitbucket; a running session can then add it straight away. |
| `ct ws sign <ID>` | On the host: sign the ticket's unpushed commits (sessions commit unsigned: they may have no signing key, e.g. when sandboxed). Run it before pushing. |
| `ct ws gc [--days N] [--dry-run]` | Free disk (on the host): workspaces of done or closed tickets idle for N days (default 14; dirty or unpushed work is kept), merged graphs of sessions that aren't running, idle kb clones, graphify snapshots older than a week. |
| `ct clean [<ID>...] [--dry-run] [--yes] [--save] [--include-done] [--keep-manual] [--purge] [--days N]` | Clean up closed tickets (on the host): remove the workspace, then move a synced ticket's folder to `.trash/<ID>/` (links to it become `[ID](<source-url>)`; files other notes link or embed move to `archive/tickets/<ID>/` first) and a manual ticket's whole folder to `archive/tickets/<ID>/` (links keep working). Skips open tickets, running sessions, uncommitted or unpushed work (commits whose changes are already in `origin/<base>`, e.g. squash-merged, count as pushed), and unsaved sessions (`--save` runs `/tickets:save` headless first); skipped ones you can act on become follow-up tasks. `done` tickets only with `--include-done`, never synced ones. |
| `ct kb repo checkout\|fetch\|graph\|ls\|reset` | Inside a `kb` session: exploration clones of any branch, tag or commit, swapped into the session's code graph. |
| `ct vault lock acquire\|release\|status\|run` | The lock `/tickets:save` holds while writing shared notes (`.vault.lock.d`). A lock idle for 30 minutes is taken over. |
| `ct vault links check\|move` | Check that wikilinks resolve; move a note without breaking links to it. |
| `ct vault groups <vault> [<slug\|group>...] [--json] [--check]` | Which projects work together (groups, `depends-on`, used-by) and which are unrelated; `--check` validates those links. Read-only. See "Projects and groups". |
| `ct graph index` | Refresh the code graphs of the main clones (`<projectsRoot>/<provider>/<owner>/<repo>`). |
| `ct graph build\|status` | The graphify image the code graph runs in (podman or docker); built when needed. |
| `ct layout [--apply]` | Move repos into `<projectsRoot>/<provider>/<owner>/<repo>`. Leaves alone a main clone that any vault's workspace borrows objects from. |

### Per-vault locations: `.workflow.json`
Each vault's tools work in their own places, so two vaults never share
workspaces (each has its own `MAN-1`) or (with tmux) a tmux session:
- `workRoot`: the ticket workspaces (`<workRoot>/<ID>`) and `kb` workspaces
  (`<workRoot>/.kb-<vault>`). Default `~/Projects/work-<vault>`.
- `projectsRoot`: the main clones. Default `~/Projects/repo-<vault>`;
  vaults can share one.

With tmux, the ticket windows run in the tmux session `tickets-<vault>`.

`ct vault configure` writes the file (`ct vault init` runs it), and `~` is
expanded. Folders that overlap another vault's (or each other) are kept
only when the user types `yes`. Paths like `~/work/<ID>` in these
instructions and in the skills stand for `<workRoot>/<ID>`, and
`~/Projects` for `<projectsRoot>`; a session's `CLAUDE.md` gives the real
ones.

### Cost guard: no paid Rovo calls
Use only the single-product Atlassian tools (JQL search, `getJiraIssue`,
Bitbucket reads), which are free. Never call `search` (unified search) or
the Teamwork Graph tools (`getGraphContext`, `getGraphObject`,
`addGraphContext`, `getTeamworkGraphContext`), and never run their
operations through `executeRead`. They cost Rovo credits (1-10 per call),
billed as overage by default from 2026-12-03. The tools are denied in
settings, but only this rule holds back `executeRead`.

### Ticket sources
`tickets/.sources.json` lists where tickets come from. Each source has
`enabled`, `idPrefix`, and, unless it's manual, the `mcp` server and `query`
ct sync uses.
- **jira**: synced. IDs are the Jira keys (`PROJ-12`).
- **manual** (`sync: false`): you write the ticket in Obsidian and ct sync
  never touches it. IDs are `MAN-<n>`.
- **servicenow** / **zendesk**: examples, disabled. Enabling one needs its MCP
  server and an adapter (`skills/ticket-sync/sources/<name>.md` in the
  claude-tickets plugin). Their IDs would be `SNOW-…` / `ZD-…`.
- Optional per source: `"model"`, the Claude model for what the command
  can't write itself (default `sonnet`), and, for jira, `"textFields"`, the
  custom text fields shown under the description, by label (default: QA
  Testing Instructions, Acceptance Criteria, Steps to Reproduce, Expected
  Result, Actual Result).

### Ticket structure (every source)
A ticket note `tickets/<ID>/<ID>.md` has three parts:
1. **Identity:** `source` (`jira`, `manual`, …), `source-id`, `summary`,
   `ticket-type` (`dev` | `bug` | `investigation` | `chore` | `epic`, which
   picks the agents' pipeline), `status`, `ignore*`, `projects` (slugs),
   `versions`, and the dependency fields below.
2. **Content, which agents never edit:**
   - a **synced** ticket has the `source-*` fields and the block between
     `<!-- source:start -->` and `<!-- source:end -->`, owned by ct sync;
   - a **manual** ticket has `## Description`, `## Acceptance criteria` and
     `## Context / links`, owned by you.
3. **Work, owned by the ticket's agents:** `## Workspace`, `## Tasks`,
   `## Questions`, `## Follow-ups`, `## Review`, `## Knowledge`, and the
   summaries `/tickets:save` appends at the end.

### Dependencies and lead tickets
Frontmatter, as wikilinks by ticket ID (`"[[PROJ-12]]"`). ct sync fills
these for synced tickets; on manual tickets you set them.
- `parent`; `children` (an epic's children, **whoever they're assigned to**);
  `blocked-by`, `blocks`, `related`; `blocked: true` while any `blocked-by`
  ticket isn't done.
- **Lead tickets:** an epic (`ticket-type: epic`) is worked as one lead ticket.
  `covers` lists the tickets its workspace does: the epic's open children
  assigned to you. Each of those has `covered-by: "[[<EPIC>]]"`, and
  `ct start` sends you to the lead (`--force` starts one on its own).
  ct sync also pulls in the parent epic of any ticket of yours (tag
  `parent-epic`), even when the epic isn't assigned to you. To group manual
  tickets, set `ticket-type: epic` and `covers` on one, and `covered-by` on
  the others.
- **Follow-ups:** at kickoff, the lead checks every open blocker, and every
  epic child that isn't yours. It lists the ones to chase under
  `## Follow-ups` as `#follow-up` tasks (who, which ticket, what's needed;
  all of them are collected in `follow-ups.md`),
  and asks you whether to wait, build on the blocker's branch, or go ahead.
  `/tickets:save` writes the results into each covered ticket too.

Lifecycle of `status`: `new` → `triage` → `in-progress` ⇄ `blocked` (waiting
on you) → `review` (ready for your review) → `done` (after `/tickets:save`). It
becomes `closed` when the source closes the ticket (set by sync) or, for a
manual ticket, when you set it. `status: blocked` always means waiting on
you; a ticket waiting on another ticket keeps its status, and the
`blocked: true` field says so.
Once closed, saved and pushed, `ct clean` takes it out of `tickets/` (to
`.trash/` for a synced ticket, `archive/tickets/` for a manual one).
`/tickets:save` sets `saved: <timestamp>` on the ticket so `ct clean` knows
its work is in the vault, and leaves a `closed` status as it is.

**Ignoring a ticket:** set `ignore: true` in its frontmatter, optionally with
`ignore-until: yyyy-MM-dd` and `ignore-reason: ...`. It is still synced, but
`ct start` skips it (unless `--force`) and the dashboard lists it under
"Ignored". Clear the flag by hand once `ignore-until` has passed.

### Sessions outside tickets
`ct claude` in a repo gives the session the default vault
(`ct vault default`) as its knowledge base, and its system prompt names the
vault and the repo's `projects/<slug>/`. A plain `claude` session knows
nothing of the vault.
The `kb` skill answers from the vault, then the code graph, then the code.
It records new or corrected facts as drafts in `inbox/`, with `target:` and
`session:` frontmatter. `/tickets:save` in that session writes the project log and
promotes the session's drafts. `/tickets:recall` works there too.

### Who writes what
Several agents work at once, so each part of the vault has one writer:
1. **ct sync** owns a synced ticket's `source-*` fields and source block.
   It also sets `status: new` on new tickets and `closed` when the source
   closes one, `.sync-state.json`, and the dependency fields it reads from
   the source (`parent`, `children`, `blocked-by`, `blocks`, `related`,
   `blocked`, and `covers` / `covered-by` between a synced epic and its
   children). It never clears a `covered-by` pointing at a manual lead
   (`MAN-<n>`), which you set. It never touches manual tickets or the work
   sections.
2. **You** own a manual ticket's content, `ignore*`, `.sources.json`, `raw/`,
   the task views (`pending.md`, `done.md`, `follow-ups.md`), and everything
   not listed here.
3. A **ticket's agents** write only inside `tickets/<ID>/` (task list,
   questions, hand-off files, `kb-drafts/`) until `/tickets:save`.
   **Exception, lead tickets:** a lead's session also writes the work
   sections (`## Workspace`, `## Tasks`, `## Questions`, `## Follow-ups`,
   `## Review`) and the `status` of the tickets it `covers`, never their
   content or `source-*` fields. Their work happens in the lead's
   workspace and they have no session of their own, so each note still has
   one writer.
   Sessions outside tickets (a repo session, `kb`) write only new drafts in
   `inbox/`, each under its own name, until `/tickets:save`.
   A `kb` session may also create a manual ticket (`ct new`, tag `from-kb`)
   for an issue it finds, writing its initial content. From then on the
   ticket is yours, like any manual ticket.
   **Unattended commands** (`ct sync`, `ct graph index`, `ct ws gc`)
   write a new `inbox/<date>-<time>-<command>-follow-ups.md` note
   (`type: follow-ups`) when a run leaves something for you: one task per
   item, so it shows in `pending.md`. Agents never edit or promote these;
   they're yours to tick off and delete.
4. **`/tickets:save`** is the only writer of `projects/`, `knowledge-base/` and
   `references/`. It holds the vault lock (`ct vault lock acquire <vault>
   <owner>`, the owner being the ticket ID, the `kb` session's id, or
   `repo-<slug>` in a repo session) while it does, re-reads notes before
   editing them, and moves drafts with `ct vault links move`.
   As its last step it sets the ticket's `saved:` timestamp (and `status:
   done`, unless the ticket is `closed`).
5. **`ct clean`** (run by you) moves the folders of the closed tickets it
   cleans (to `.trash/` or `archive/tickets/`) and rewrites links to a
   removed ticket in any note to `[ID](<source-url>)`, under the vault lock,
   one ticket at a time. It never edits the sync-owned parts of synced
   tickets (their dependency fields and source block: ct sync writes those
   back, so a link there to a removed ticket stays, and `ct vault links
   check` doesn't count it). Tickets it skips leave tasks in
   `inbox/<date>-<time>-ct-clean-follow-ups.md`.

### Roles
The `/tickets:work-ticket` lead runs role subagents, chosen by `ticket-type`:
- `dev`: product-owner → architect (design) → developer → qa
- `bug`: architect (trace) → developer (fix + regression test) → qa
- `investigation`: architect (trace) → developer (dig in; fix only if asked)
- `chore`: developer → qa if behaviour can change
- `epic`: lead: plan across the covered tickets, then each one's own pipeline

It always asks you for the repos (local ones from the code graph, plus related
Bitbucket repos that aren't cloned yet, which you clone with `ct ws clone`),
the base branch and the target version per repo at
kickoff. It stops to ask (status `blocked`, desktop notification) on open
questions or major tradeoffs, and stops at `review` with the work committed
(unsigned) on the ticket branches, never pushed: you review, run `/tickets:save`,
then `ct ws sign <ID>` and push yourself. Manual and synced
tickets go through the same flow.

## Session Commands (skills of the claude-tickets plugin)

### /tickets:recall `[project|ticket]`
(Called `/resume` before. Renamed so it doesn't shadow Claude Code's built-in
`/resume`, which reopens past conversations.)
When you receive this command:
1. Read the 3 most recent session logs in projects/`<slug>`/logs/ (or for a ticket: its logs, task list, and each of its projects)
2. Read architecture/`<slug>`-decisions.md for the current project, and `<group>`-decisions.md of each group it belongs to (`ct vault groups <vault> <slug>`)
3. Summarize current state and what's left to do

### /tickets:save
When you receive this command:
1. Create a session log in projects/`<slug>`/logs/yyyy-MM-dd-description.md (in a ticket workspace, also `tickets/<ID>/logs/`)
2. Record: what was done, decisions made, pending items
3. Add wikilinks to created/modified notes

In a ticket workspace (`~/work/<ID>`), `/tickets:save` also does what `/summarize`
did:
4. Promotes `tickets/<ID>/kb-drafts/` into `projects/<slug>/…`,
   `projects/<group>/…`, `knowledge-base/` and `references/`. This covers the
   projects touched by the feature, the versions it shipped in, the
   dependencies between projects (`depends-on`), and the flows across
   projects as mermaid diagrams. It asks you before creating a group, and
   runs `ct vault links check` and `ct vault groups --check` on the notes
   it wrote (problems elsewhere are reported, not fixed). Links stay
   intact.
5. Tags and links all documentation updated in the session with the ticket.
6. Adds tags `[slug]-[release-version]` to tickets/`ticket` for every
   project modified (dots as dashes, e.g. `bitbucket-sops-orders-service-2-4-0`,
   plus the exact `versions: [bitbucket-sops-orders-service@2.4.0]` field).
7. Appends a summary of the changes at the end of tickets/`ticket`:
```
---
created: *yyyy-MM-ddTHH:mm:ss*

summary here
```

### /summarize `ticket`
Folded into `/tickets:save`, which runs it automatically in a ticket workspace.

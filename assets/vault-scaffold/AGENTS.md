# Obsidian Vault - Instructions for Agents

## What is this vault
Centralized knowledge base for all projects. Persistent memory across sessions. It is also the
tracking surface for tickets (from Jira, created by hand, or any other source) worked by
parallel, sandboxed agents (see **Ticket workflow**).

## Vault Structure
```
ROOT
├── .workflow.json         # where this vault's tools work: workRoot, projectsRoot (see "Ticket workflow")
├── .scaffold.json         # what ct shipped here (ct vault update); don't edit
├── raw/, templates/       # unorganized manual docs (agents: ignore); templates for the note types
├── inbox.md               # open tasks in inbox/ (Tasks plugin; agents: ignore)
├── tickets.base, projects.base # Bases dashboards of tickets and of projects/
├── tickets/               # one folder per ticket, from any source
│   ├── .sources.json      #   ticket sources (see "Ticket sources")
│   └── PROJ-12/           #   one ticket, named by its ID
│       ├── PROJ-12.md     #     ticket note: content, workspace, tasks, questions, review, summaries
│       ├── PROJ-12-status.md # live task views of the folder (Tasks plugin; never edited)
│       ├── plan.md        #     the lead's plan: scope, criteria, decisions, waves, relationships
│       ├── board.md       #     one line per task and review; the PO's lead: and po:
│       ├── relay.md       #     you <-> PO <-> lead: requests, questions, answers, decisions (capped)
│       ├── backlog.md     #     leftovers and review findings not fixed yet (≤40 open)
│       ├── tasks/         #     PROJ-12-NN-<slug>.md: one task each, with the agent's ## Report
│       ├── contracts/     #     <name>-v<N>.md: exact shapes that tasks share
│       ├── reviews/       #     wave-N-<slug>.md: one capped review per wave and repo, ending in `## Dispositions`
│       └── kb-drafts/, logs/, investigation.md, pr-feedback.md, PROJ-12-questionnaire-<slug>.md
├── learning/<topic>/      # one folder per topic you learn (ct teach; see "Learning topics and finding context")
├── inbox/                 # knowledge drafts from non-ticket sessions (promoted by /tickets:save),
│                          #   and follow-up notes (type: follow-ups) unattended commands leave for you
├── knowledge-base/, references/, logs/, archive/ # findings, reference material, global logs, ct clean's leavings (never edit)
├── projects/              # one project per repo, named by its slug; optional groups (below)
│   ├── <slug>/            #   <slug>.md index (project.md), architecture/ (+ <slug>-decisions.md),
│   │                      #   sequences/ (mermaid), data/, features/, logs/
│   └── <group>/           #   (optional) <group>.md (group.md), architecture/ (<group>-decisions.md,
│                          #   <group>-compatibility.md), sequences/, features/, logs/
└── AGENTS.md              # global instructions for AI Agents
```

### Repo slugs
Every repo is named by its **slug**, `<provider>-<owner>-<repo>` in kebab-case, from its git origin
(`bitbucket-acme-billing-service`, `github-ato-dotfiles`). It names the project folder and notes,
the `projects:` and `versions:` entries of tickets, version tags, worktree folders and graph node
ids; `ct ws repos` lists every slug. Project index notes carry `aliases: [<repo>]`.

### Projects and groups
Every repo is a **project**, `projects/<slug>/`, and stands alone unless its notes say otherwise.
- **`depends-on`** (index note frontmatter, `["[[<other-slug>]]", …]`): the projects of this
  vault this one needs at run, build or deploy time (`A depends-on B`: a change to B can break
  A; a two-way interaction gets a link on each side). The `## Depends on` table says how and
  since which version. "Used by" isn't stored: backlinks show it.
- **`groups`** (`["[[<group>]]", …]`): the groups the project belongs to. Missing or empty: none.
- **Groups** are optional: `projects/<group>/<group>.md` (`type: group`) holds what several
  projects share (`## Interactions` in mermaid, kept in step with the members' `depends-on`;
  flows; `<group>-compatibility`; `<group>-decisions`). Members link the group through
  `groups`; groups are flat; a name is kebab-case, unique, never a repo slug. Only
  `/tickets:save` creates one, after asking you.
- Projects linked through `depends-on` or `groups`, directly or not, form a **cluster**
  (related); a project without links is standalone. `ct vault groups <vault> [<slug|group>...]
  [--json] [--check]` prints clusters or one project's links and validates them.

## Note Rules

### Creation
- Wikilinks `[[note-name]]`, never markdown links; link by **bare note name**, never by path.
- Every **filename is unique across the vault** (bare-name links depend on it): projects are
  named by slug, groups by a name that is never a slug, decision indexes `<slug>-decisions.md`
  (`<group>-decisions.md`), session logs `yyyy-MM-dd-<ID>-<slug>-<description>.md`.
- Mandatory YAML frontmatter on every note; filenames and tags in kebab-case (no dots in tags).
- 1 concept per permanent note; at least 2 wikilinks per note. Use the template of the note's
  type (below), else `base.md`; `{{date}}` is `yyyy-MM-dd`, `{{title}}` the note title.

### Templates
`templates/<type>.md`, one per note type: `base`, `session-log`, `ticket-synced`, `ticket-manual`,
`project`, `group`, `feature`, `sequence` (mermaid flows), `decision`, `investigation`,
`knowledge`, `reference`, `pr-feedback`, `learning-topic`, `lesson`, `learning-record`,
`learning-reference`, and for `tickets/<ID>/` `plan`, `board`, `relay`, `backlog`, `status`
(`<ID>-status.md`), `task` and `review`.

### Standard frontmatter
`title`, `created`, `updated` (`yyyy-MM-dd`), `status: active`, `type: permanent`,
`tags: [project, topic1, ...]`; the template of the note's type has the rest.

### Never do
Don't delete notes without asking (`ct clean`, run by you, is the only way ticket notes leave the
vault), use markdown links for internal notes, create notes without frontmatter, change the
folder structure without documenting it, or move notes with `mv` (use `ct vault links move`).

### Task lists
Task states (Tasks plugin): `- [ ]` todo, `- [/]` in progress, `- [x]` done, `- [-]` cancelled,
each followed by the task name, `➕ yyyy-MM-dd` when created and `✅ yyyy-MM-dd` when done
(`❌ yyyy-MM-dd` when cancelled), e.g. `- [x] task name ➕ 2026-10-25 ✅ 2026-10-26`.
- **Checkboxes go only in:** the ticket note's `## Tasks` (one line per phase or wave, tagged
  `#ticket/<phase>`: `kickoff`, `plan`, `wave`, `handover`) and `## Follow-ups`, `board.md
  ## Tasks`, a review's `## Dispositions`, and `backlog.md`.
- **Everywhere else** in `tickets/<ID>/` (plan, task files, relay, contracts, kb-drafts,
  investigation, logs, review finding sections, `board.md ## Reviews`) **plain bullets only**:
  the status file lists every checkbox in the folder. No line inside an HTML comment starts
  with `- [`.
- On `board.md` and in `## Dispositions` the status or disposition **word** decides the state;
  the checkbox follows it.

## Ticket workflow
The tools come from claude-tickets: the `ct` commands (`ct --help`, `ct <command> --help`) and a
Claude Code plugin (`tickets`) with the skills (`/tickets:<skill>`), the task and review agents,
and the hooks. A ticket runs as a **PO** session (`/tickets:work-ticket <ID>`, the one you talk
to) with one **lead** agent that plans and merges in waves of task agents, each wave reviewed;
state lives in `tickets/<ID>/`. How it works (kickoff, plan, contracts, waves, review,
hand-over, `relay.md`, the agents), the dependency fields, lead tickets and the status
lifecycle: `work-ticket/workflow.md`, "Workflow", and the skills `work-ticket`, `lead`,
`task-agent` and `wave-review`. The skills ship with the plugin, not in the vault; in a session
they run as `/tickets:<skill>` (e.g. `/tickets:work-ticket`). Session commands: `/tickets:save` (session log, promotes drafts,
appends the summary), `/tickets:recall` (read-only recap), `/tickets:kb` (answers from the vault,
drafts into `inbox/`) and `/tickets:teach` (learning topics).
Paths: `workRoot` (in `.workflow.json`) holds the ticket workspaces `<workRoot>/<ID>`, `projectsRoot` the
main clones; `~/work/<ID>` in skills stands for the former, `~/Projects` for the latter (a session's
`CLAUDE.md` gives the real paths).

### Cost guard: no paid Rovo calls
Use only the single-product Atlassian tools (JQL search, `getJiraIssue`, Bitbucket reads), which
are free. Never call `search` (unified search) or the Teamwork Graph tools (`getGraphContext`,
`getGraphObject`, `addGraphContext`, `getTeamworkGraphContext`), nor run their operations through
`executeRead`: they cost Rovo credits. The tools are denied in settings, but only this rule
holds back `executeRead`.

### Ticket sources
`tickets/.sources.json` lists where tickets come from. Each source has `enabled`, `idPrefix`,
and, unless it's manual, the `mcp` server and `query` ct sync uses.
- **jira**: synced. IDs are the Jira keys (`PROJ-12`).
- **manual** (`sync: false`): you write it in Obsidian, ct sync never touches it; IDs `MAN-<n>`.
- **servicenow** / **zendesk**: examples, disabled (an MCP server and an adapter in `skills/ticket-sync/sources/` enable one).
- Optional per source: `"model"` (default `sonnet`) and, for jira, `"textFields"`.

### Ticket structure (every source)
A ticket note `tickets/<ID>/<ID>.md` has three parts:
1. **Identity:** `source`, `source-id`, `summary`, `ticket-type` (`dev` | `bug` | `investigation`
   | `chore` | `epic`), `status` (`new`, `triage`, `in-progress`, `blocked` = waiting on you,
   `review`, `done`, `closed`), `ignore*`, `projects` (slugs), `versions`, and the dependency
   fields (`parent`, `children`, `blocked-by`, `blocks`, `related`, `blocked`, `covers`, `covered-by`).
2. **Content, which agents never edit:** a synced ticket's `source-*` fields and source block
   (`<!-- source:start -->` to `<!-- source:end -->`); a manual ticket's `## Description`,
   `## Acceptance criteria` and `## Context / links`.
3. **Work, owned by the ticket's agents:** `## Workspace`, `## Tasks`, `## Questions`,
   `## Follow-ups`, `## Review`, `## Knowledge`, and the summaries `/tickets:save` appends.

### Learning topics and finding context
`ct teach <topic>` runs `/tickets:teach <topic>`; its state is `learning/<topic>/` (the `teach`
skill has the layout; related repos go in `projects:`).

Search before reading: `ct vault search "<question>" --budget 2000` (add `--project <slug>` or
`--path <folder>`), then `ct vault note outline <note>` and `ct vault note get <note>
"<section>"`, and `ct vault related <note>` for neighbours. Read a whole note only to edit it.
`.ct/` is a rebuildable index: never edit it.

### Who writes what
Several agents work at once, so each part of the vault has one writer. `ct vault note` enforces
these rules (it refuses a write outside the session's area with exit 3; `/tickets:save` passes
`--save`):
1. **ct sync** owns a synced ticket's `source-*` fields and source block, `status: new` on new
   tickets and `closed` when the source closes one, `.sync-state.json`, and the dependency fields
   it reads from the source (incl. `covers` / `covered-by` between a synced epic and its
   children; it never clears a `covered-by` pointing at a manual lead). It never touches manual
   tickets or the work sections.
2. **You** own a manual ticket's content, `ignore*`, `.sources.json`, `raw/`, `inbox.md`, and
   everything not listed here.
3. A **ticket's agents** write only inside `tickets/<ID>/` until `/tickets:save`. The **PO**
   writes the note's `## Questions`, `status` (it runs the gate; the lead also sets it as it
   plans) and kickoff fields (`## Workspace`, `projects:`, `ticket-type`), `relay.md` (the lead
   appends its own questions and sets any entry's state), the questionnaires, the first
   `board.md`, `relay.md` and `<ID>-status.md` from their templates (never overwriting one),
   `board.md`'s `lead:`, `lead-since:` and `po:`, and the `orphaned` mark on lines of an ended
   session. The **lead** writes the rest of the folder: the other work sections, `plan.md`,
   `board.md`, `backlog.md`, the task files, `contracts/` and `kb-drafts/`. A task agent writes
   only its own task file's `## Report` (plus the code in its `owns`); a reviewer only its
   `reviews/` file; an analyst nothing.
   Agents never file a follow-up ticket on their own: the lead asks you, for items it would not
   fix in this ticket (options: A, this ticket; B, a new ticket; C, an existing ticket (name it); D, leave it in the backlog (backlog items only)), and only for a new ticket runs
   `ct new` (tag `from-ticket`), writing its initial content.
   **Exception, lead tickets:** a lead's session also writes the work sections (`## Workspace`,
   `## Tasks`, `## Questions`, `## Follow-ups`, `## Review`) and the `status` of the tickets it
   `covers`, never their content or `source-*` fields.
   Sessions outside tickets (a repo session, `kb`) write only new drafts in `inbox/`, each under
   its own name, until `/tickets:save`; a `kb` session may create a manual ticket (`ct new`, tag
   `from-kb`) once you have said yes, and it is yours from then on. A **teach session** owns only
   `learning/<topic>/` and writes it directly, one session per topic, holding the vault lock
   (`ct vault lock acquire <vault> teach-<topic>`) around writes that touch more than one file.
   **Unattended commands** (`ct sync`, `ct graph index`, `ct ws gc`) write a new
   `inbox/<date>-<time>-<command>-follow-ups.md` note (`type: follow-ups`) when a run leaves
   something for you: one task per item, listed in `inbox.md`. Agents never edit or promote
   these; they're yours to tick off and delete.
4. **`/tickets:save`** is the only writer of `projects/`, `knowledge-base/` and `references/`.
   It holds the vault lock (`ct vault lock acquire <vault> <owner>`: the ticket ID, the `kb`
   session's id, or `repo-<slug>`) while it does, re-reads notes before editing them, and moves
   drafts with `ct vault links move`. Its last step sets the ticket's `saved:` timestamp (and
   `status: done`, unless the ticket is `closed`).
5. **`ct clean`** (run by you) moves the folders of the closed tickets it cleans (to `.trash/`
   or `archive/tickets/`) and rewrites links to a removed ticket in any note to
   `[ID](<source-url>)`, under the vault lock. It never edits the sync-owned parts of synced
   tickets. Tickets it skips leave tasks in `inbox/<date>-<time>-ct-clean-follow-ups.md`.

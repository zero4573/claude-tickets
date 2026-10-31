# Obsidian Vault - Instructions for Agents

## What is this vault
Centralized knowledge base for all projects. Persistent memory across sessions. It is also the
tracking surface for tickets (from Jira, created by hand, or any other source) worked by
parallel, sandboxed agents (see **Ticket workflow**).

## Vault Structure
```
ROOT
├── .workflow.json         # where this vault's tools work: workRoot, projectsRoot (see "Ticket workflow")
├── .scaffold.json         # what tix shipped here (tix vault update); don't edit
├── raw/, templates/       # unorganized manual docs (agents: ignore); templates for the note types
├── inbox.md               # open tasks in inbox/ (Tasks plugin; agents: ignore)
├── tickets.base, projects.base # Bases dashboards of tickets and of projects/
├── tickets/               # a folder per source (its key in .sources.json: jira, manual, ...), a folder per ticket in it
│   ├── .sources.json      #   ticket sources (see "Ticket sources")
│   └── jira/PROJ-12/      #   one ticket: <source>/<ID>/ (the source is the note's `source:`)
│       ├── PROJ-12.md     #     ticket note: content, workspace, tasks, questions, review, summaries
│       ├── PROJ-12-status.md # live task views of the folder (Tasks plugin; never edited)
│       ├── plan.md        #     the lead's plan: scope, criteria, decisions, waves, relationships
│       ├── board.md       #     one line per task and review (older: board-archive.md); the PO's lead: and po:
│       ├── relay.md       #     you <-> PO <-> lead: requests, questions, answers, decisions (capped)
│       ├── backlog.md     #     leftovers and review findings not fixed yet (≤40 open)
│       ├── tasks/         #     PROJ-12-NN-<slug>.md: one task each, with the agent's ## Report
│       ├── contracts/     #     <name>-v<N>.md: exact shapes that tasks share
│       ├── reviews/       #     wave-N-<slug>.md: one capped review per wave and repo, ending in `## Dispositions`
│       ├── spec-draft.md, specs/<slug>/ # the spec change in full; the PO's copy of a vault spec (see "Specs")
│       └── kb-drafts/, logs/, conventions.md, features-and-deviations.md, investigation.md, pr-feedback.md, PROJ-12-questionnaire-<slug>.md
├── learning/<topic>/      # one folder per topic you learn (tix teach; see "Learning topics and finding context")
├── inbox/                 # knowledge drafts from non-ticket sessions (promoted by /tix:save),
│                          #   and follow-up notes (type: follow-ups) unattended commands leave for you
├── knowledge-base/, references/, logs/, archive/ # findings, reference material, global logs, tix clean's leavings (never edit)
├── projects/              # one project per repo, named by its slug; optional groups (below)
│   ├── <slug>/            #   <slug>.md index (project.md), architecture/ (+ <slug>-decisions.md),
│   │                      #   specs/<capability>/spec.md (only when `specs: vault`), features/ (an
│   │                      #   index of tickets and versions), logs/
│   └── <group>/           #   (optional) <group>.md (group.md), architecture/ (<group>-decisions.md,
│                          #   <group>-compatibility.md), sequences/ (flows across members), logs/
└── AGENTS.md              # global instructions for AI Agents
```

### Specs
A repo's specs are `specs/<capability>/spec.md` in the repo, unless its project note says
`specs: vault`: then they live at `projects/<slug>/specs/` (same layout) and nothing spec-related
goes into the repo. Empty `specs:` means nobody asked yet: the first reader that finds no
`specs/` in the repo asks you once and records it with `tix spec location <slug> [repo|vault]`.
Single-repo `sequences/` and `data/` notes and the feature note as a description are retired
(existing notes stay).

### Repo slugs
Every repo is named by its **slug**, `<provider>-<owner>-<repo>` in kebab-case, from its git origin
(`bitbucket-acme-billing-service`, `github-ato-dotfiles`). It names the project folder and notes,
the `projects:` and `versions:` entries of tickets, version tags and worktree folders; `tix ws repos` lists every slug. Project index notes carry `aliases: [<repo>]`.

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
  `/tix:save` creates one, after asking you.
- Projects linked through `depends-on` or `groups`, directly or not, form a **cluster**
  (related); a project without links is standalone. `tix vault groups <vault> [<slug|group>...]
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
`project`, `group`, `feature` (an index), `sequence` (mermaid, across a group's projects), `decision`, `investigation`, `knowledge`,
`reference`, `pr-feedback`, `learning-topic`, `lesson`, `learning-record`, `learning-reference`,
and for `tickets/<source>/<ID>/` `spec-draft`, `plan`, `board`, `relay`, `backlog`, `status`, `task` and `review`.

### Standard frontmatter
`title`, `created`, `updated` (`yyyy-MM-dd`), `status: active`, `type: permanent`,
`tags: [project, topic1, ...]`; the template of the note's type has the rest.

### Vault-only IDs
Task numbers, waves, relay `R-nn`, backlog `B-nn`, review finding IDs and a manual ticket's key
(`MAN-<n>`) live only in this vault: never in code, comments, test names, docs, commit messages,
pushed branch names or PR text. Only a synced ticket's key (`PROJ-12`) may appear there.

### Never do
Don't delete notes without asking (only `tix clean`, run by you, removes ticket notes), use markdown
links for internal notes, create notes without frontmatter, change the folder structure without
documenting it, or move notes with `mv` (use `tix vault links move`).

### Task lists
Task states (Tasks plugin): `- [ ]` todo, `- [/]` in progress, `- [x]` done, `- [-]` cancelled,
each followed by the task name, `➕ yyyy-MM-dd` when created and `✅ yyyy-MM-dd` when done
(`❌ yyyy-MM-dd` when cancelled), e.g. `- [x] task name ➕ 2026-10-25 ✅ 2026-10-26`.
- **Checkboxes go only in:** the ticket note's `## Tasks` (one line per phase or wave, tagged
  `#ticket/<phase>`: `kickoff`, `plan`, `wave`, `handover`) and `## Follow-ups`, `board.md
  ## Tasks` (and `board-archive.md ## Tasks`), a review's `## Dispositions`, and `backlog.md`.
- **Everywhere else** in `tickets/<source>/<ID>/` (plan, task files, relay, contracts, kb-drafts,
  investigation, logs, review finding sections, `board.md ## Reviews`) **plain bullets only**:
  the status file lists every checkbox in the folder. No line inside an HTML comment starts
  with `- [`.
- On `board.md` and in `## Dispositions` the status or disposition **word** decides the state;
  the checkbox follows it.

## Ticket workflow
The tools come from tix: the `tix` commands (`tix --help`, `tix <command> --help`) and a
Claude Code plugin (`tickets`) with the skills (`/tickets:<skill>`), the task and review agents,
and the hooks. A ticket runs as a **PO** session (`/tix:work-ticket <ID>`, the one you talk
to) with one **lead** agent that plans and merges in waves of task agents, each wave reviewed;
state lives in `tickets/<source>/<ID>/`. How it works (kickoff, plan, contracts, waves, review,
hand-over, `relay.md`, the agents), the dependency fields, lead tickets and the status
lifecycle: `work-ticket/workflow.md`, "Workflow", and the skills `work-ticket`, `lead`,
`task-agent` and `wave-review`. The skills ship with the plugin, not in the vault. Session
commands: `/tix:save` (session log, promotes drafts, appends the summary), `/tix:recall` (read-only recap), `/tix:spec` (answers from the vault,
drafts into `inbox/`) and `/tix:teach` (learning topics). Paths: `workRoot` (in `.workflow.json`) holds the ticket workspaces `<workRoot>/<ID>`, `projectsRoot` the
main clones; `~/work/<ID>` in skills stands for the former, `~/Projects` for the latter (a session's
`CLAUDE.md` gives the real paths).

### Cost guard: no paid Rovo calls
Use only the single-product Atlassian tools (JQL search, `getJiraIssue`, Bitbucket reads; free). Never call `search` (unified search) or the Teamwork Graph tools (`getGraphContext`,
`getGraphObject`, `addGraphContext`, `getTeamworkGraphContext`), nor run their operations through
`executeRead`: they cost Rovo credits. Settings deny the tools; only this rule holds back `executeRead`.

### Ticket sources
`tickets/.sources.json` lists where tickets come from. Each source has `enabled`, `idPrefix`,
and, unless it's manual, the `mcp` server and `query` tix sync uses.
- **jira**: synced. IDs are the Jira keys (`PROJ-12`).
- **manual** (`sync: false`): you write it in Obsidian, tix sync never touches it; IDs `MAN-<n>`.
- **servicenow** / **zendesk**: examples, disabled (an MCP server and an adapter in `skills/ticket-sync/sources/` enable one).
- Optional per source: `"model"` (default `sonnet`) and, for jira, `"textFields"`.

### Ticket structure (every source)
A ticket note `tickets/<source>/<ID>/<ID>.md` has three parts:
1. **Identity:** `source`, `source-id`, `summary`, `ticket-type` (`dev` | `bug` | `investigation`
   | `chore` | `epic`), `status` (`new`, `triage`, `in-progress`, `blocked` = waiting on you,
   `review`, `done`, `closed`), `ignore*`, `projects` (slugs), `versions`, and the dependency
   fields (`parent`, `children`, `blocked-by`, `blocks`, `related`, `blocked`, `covers`, `covered-by`).
2. **Content:** a synced ticket's `source-*` fields and source block (`<!-- source:start -->` to
   `<!-- source:end -->`): agents never edit. A manual ticket's `## Description`, `## Acceptance
   criteria`, `## Context / links`: yours; its own agents may record your decisions or tick
   met criteria (enforced: `source: manual` only, never a covered ticket).
3. **Work, owned by the ticket's agents:** `## Workspace`, `## Tasks`, `## Questions`,
   `## Follow-ups`, `## Review`, `## Knowledge`, and the summaries `/tix:save` appends.

### Learning topics and finding context
`tix teach <topic>` runs `/tix:teach <topic>`; its state is `learning/<topic>/` (the `teach`
skill has the layout; related repos go in `projects:`).

Search before reading: `tix vault search "<question>" --budget 2000` (add `--project <slug>` or
`--path <folder>`), then `tix vault note outline <note>` and `tix vault note get <note>
"<section>"`, and `tix vault related <note>` for neighbours. Read a whole note only to edit it.
`.tix/` is a rebuildable index: never edit it.

### Who writes what
Several agents work at once, so each part of the vault has one writer. `tix vault note` enforces
these rules (a write outside the session's area is refused, exit 3; `/tix:save` passes `--save`):
1. **tix sync** owns a synced ticket's `source-*` fields and source block, `status: new` on new
   tickets and `closed` when the source closes one, `.sync-state.json`, and the dependency fields
   it reads from the source (incl. `covers` / `covered-by` between a synced epic and its
   children; it never clears a `covered-by` pointing at a manual lead). It never touches manual
   tickets or the work sections.
2. **You** own a manual ticket's content (see Ticket structure 2), `ignore*`, `.sources.json`,
   `raw/`, `inbox.md`, and everything not listed here.
3. A **ticket's agents** write only inside `tickets/<source>/<ID>/` until `/tix:save`. The **PO**
   writes the note's `## Questions`, `status` (it runs the gate; the lead also sets it as it
   plans) and kickoff fields (`## Workspace`, `projects:`, `ticket-type`), `relay.md` (the lead
   appends its own questions and sets any entry's state), the questionnaires, the first
   `board.md`, `relay.md` and `<ID>-status.md` from their templates (never overwriting one),
   `board.md`'s `commit-style:`, `lead:`, `lead-since:` and `po:`, and the `orphaned` mark on
   lines of an ended session. The **lead** writes the rest of the folder: the other work sections, `plan.md`,
   `board.md`, `backlog.md`, the task files, `contracts/` and `kb-drafts/`. A task agent writes
   only its own task file's `## Report` (plus the code in its `owns`); a reviewer only its
   `reviews/` file; an analyst nothing.
   **Vault specs:** a ticket session never writes `projects/<slug>/specs/`: the PO edits a copy under
   `tickets/<source>/<ID>/specs/<slug>/…`, and `/tix:save` promotes it (the copy against
   `projects/` is the change). The PO writes the note's `specs:` (`<slug>/<capability>`) and
   `requirements:` (IDs); `tix sync` keeps them. The one write outside the ticket folder is the
   project note's `specs:` field, through `tix spec location`, after your answer.
   Agents never file a follow-up ticket on their own: the lead asks you, for items it would not
   fix in this ticket (options: A, this ticket; B, a new ticket; C, an existing ticket (name it); D, leave it in the backlog (backlog items only)), and only for a new ticket runs
   `tix new` (tag `from-ticket`), writing its initial content.
   **Exception, lead tickets:** a lead's session also writes the work sections (`## Workspace`,
   `## Tasks`, `## Questions`, `## Follow-ups`, `## Review`) and the `status` of the tickets it
   `covers`, never their content or `source-*` fields.
   Sessions outside tickets (a repo session, `spec`) write only new drafts in `inbox/`, each under
   its own name, until `/tix:save`; a `spec` session may create a manual ticket (`tix new`, tag
   `from-spec`) once you have said yes, and it is yours from then on. A **teach session** owns only
   `learning/<topic>/` and writes it directly, one session per topic, holding the vault lock
   (`tix vault lock acquire <vault> teach-<topic>`) around writes that touch more than one file.
   **Unattended commands** (`tix sync`, `tix ws gc`) write a new
   `inbox/<date>-<time>-<command>-follow-ups.md` note (`type: follow-ups`) when a run leaves
   something for you: one task per item, listed in `inbox.md`. Agents never edit or promote
   these; they're yours to tick off and delete.
4. **`/tix:save`** is the only writer of `projects/`, `knowledge-base/` and `references/`.
   It holds the vault lock (`tix vault lock acquire <vault> <owner>`: the ticket ID, the `spec`
   session's id, or `repo-<slug>`) while it does, re-reads notes before editing them, and moves
   drafts with `tix vault links move`. Its last step sets the ticket's `saved:` timestamp (and
   `status: done`, unless the ticket is `closed`).
5. **`tix clean`** (run by you) moves the folders of the closed tickets it cleans (to `.trash/`
   or `archive/tickets/<source>/`) and rewrites links to a removed ticket in any note to
   `[ID](<source-url>)`, under the vault lock. It never edits the sync-owned parts of synced
   tickets. Tickets it skips leave tasks in `inbox/<date>-<time>-tix-clean-follow-ups.md`.

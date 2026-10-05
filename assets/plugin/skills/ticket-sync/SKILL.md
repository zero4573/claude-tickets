---
name: ticket-sync
description: Sync the user's open tickets from one ticket source (jira, ...) listed in the vault's tickets/.sources.json into the Obsidian vault's tickets/ folder, one note per ticket, skipping unchanged tickets and closing ones the source has closed. Run headless as /tickets:ticket-sync <source> by the ct sync command from the vault root; use when asked to sync or pull tickets into the vault.
---

# /tickets:ticket-sync <source>

You run unattended from the root of an Obsidian vault (the cwd), with the
source's MCP server (read-only) and file tools. Nobody can answer
questions, so never ask any. On an error, skip that ticket, note it in the
summary, and carry on. Never modify the source system.

Use only the MCP server named by the source's `mcp`, never another
connector. Take every setting (query, prefix, and optional ones such as a
site) from `.sources.json`, and never guess one. The adapter says how to
fill in an optional setting that isn't there. If a required setting is
missing, or the source rejects the credentials (401/403), stop and report
exactly what to fix: there's nothing useful to sync without them.

- **No paid Rovo calls** (the vault's `AGENTS.md`, "Cost guard"): never
  `search` or the Teamwork Graph tools, not even through `executeRead`.

Read first:
- `tickets/.sources.json`, for this source's settings: `mcp`, `query` and
  `idPrefix`. If the source is missing, disabled, or has `"sync": false`
  (e.g. `manual`), stop and say so.
- `sources/<source>.md` next to this file: the adapter, which says how to
  query this source and map its fields. If there's no adapter for the
  source, stop and say so.
- The vault's `AGENTS.md`, for note rules and the ticket structure, and
  `templates/ticket-synced.md`, for the note layout.

## Plan mode: `/tickets:ticket-sync <source> --plan <file> --followups <file>`

For jira, the `ct sync` command syncs by itself, without a model. It
reconciles every note against Jira, reads each changed ticket's changelog,
and rewrites only the part of the note that changed: the frontmatter and the
source block's table, `### Description`, `### Recent comments`, or an epic's
`### Children`. You're started only for what it can't write: a description
or comments that Jira returns as HTML, because Markdown can't represent them
(panels, @mentions, media, expands). `tickets/.sync-plan.json` lists them:

```json
{"source": "jira", "cloudId": "...", "accountId": "...",
 "html": [{"id": "PROJ-63", "part": "description"},
          {"id": "PROJ-70", "part": "comments", "marker": "<!-- comments: 881@2026-10-03T10:00:00.000+0000 -->"}]}
```

For each entry, and only those:
- Fetch that part with the plan's `cloudId`:
  - `description`: `getJiraIssue` with `view: "evidence"`. The description is
    `fields.description`, plus the text custom fields under
    `fields.customFields`.
  - `comments`: `listJiraIssueComments` through `executeRead`, with
    `maxResults: 5` and `orderBy: "-created"`.
- Convert it to Markdown that Obsidian renders: tables, lists and code blocks
  as Markdown; panels as `> **Note:** ...` callouts; mentions as plain names;
  media as their file names.
- Replace only that section inside the note's source block, keeping its
  heading:
  - `### Description`: the description, then each text custom field as
    `**<label>:** <value>`.
  - `### Recent comments`: `- **<author>**, yyyy-MM-dd: <text>`, oldest of
    the five first, each trimmed to about 15 lines, with continuation lines
    indented two spaces. End the section with the entry's `marker` line,
    exactly as given: the command compares it on the next run.
- Touch nothing else in the note: the command has already written the rest.

Don't list, open or compare tickets that aren't in the plan, and don't
write `.sync-state.json`.

Whatever needs the user (a ticket you couldn't fetch or write, anything odd
in the source), append one line per item to the `--followups` file. Write it
as a task, without the checkbox: `Check [[PROJ-63]]: <what's wrong and what
to do>`. The command turns the lines into a follow-up note in `inbox/`.

Without `--plan` (a source with no command-side sync, or
`ct sync --full`), follow every step below, rendering notes in the same
layout the command writes (see **The source block**). The `--followups`
file works the same way if it's given.

## Ticket IDs

The local ID names the folder, the note, the branch (`feature/<ID>`) and
the workspace (`~/work/<ID>`). With an empty `idPrefix` it's the source's
native key as-is (Jira `PROJ-12`); otherwise it's
`<idPrefix>-<native id>` (`SNOW-INC0012345`). It must match
`^[A-Z][A-Z0-9_]*(-[A-Z0-9]+)+$`. Upper-case it and replace other
characters with `-` if needed. The note is `tickets/<ID>/<ID>.md`.

## What you own in a ticket note

Only notes with `source: <this source>`. Within those, write **only**:
- frontmatter: `title`, `summary`, `source`, `source-id`, `source-url`,
  `source-type`, `source-status`, `source-status-category`,
  `source-priority`, `source-assignee`, `source-fix-versions`,
  `source-updated`, `updated`
- the dependency fields: `parent`, `children`, `blocked-by`, `blocks`,
  `related`, `blocked`, plus `covers` on epics and `covered-by` on their
  children (see **Dependencies and epics**)
- `status`, only as described below
- `ticket-type` and `created`, only when creating the note
- the block between `<!-- source:start -->` and `<!-- source:end -->`
- the `unassigned` tag

Leave everything else exactly as it is: `ignore`, `ignore-until`,
`ignore-reason`, `projects`, `versions`, other tags, and every other
section. **Ignored tickets are synced like any other.** `ignore` only stops
them being launched.

Normalized values:
- `source-status-category`: `todo`, `in-progress` or `done`. The adapter
  says how to map to it. Closing depends only on this.
- `source-updated`: the source's last-modified timestamp, verbatim. The
  next sync compares against it.
- `ticket-type`, set on creation: `dev`, `bug`, `investigation`, `chore`
  or `epic`, mapped by the adapter.

## Steps

1. **State.** Read `tickets/.sync-state.json` if it exists:
   `{"sources": {"<source>": {"lastSync": "<iso>", ...}}}`. Get the time
   now (`date -Iseconds`).
2. **Open tickets.** Run the source's query from `.sources.json` through the
   adapter, paging through every result, and fetch only the ID, the
   last-updated time and the status.
3. **For each result**, find the local note
   (`tickets/<ID>/<ID>.md`) and compare its `source-updated`:
   - **Same or later:** skip it. It's unchanged, unless the note has no
     `blocked-by` field yet (written before dependencies were tracked): then
     treat it as older, so the dependency fields get filled in once.
   - **No note:** fetch the full ticket and create the note from
     `templates/ticket-synced.md`:
     - `status: new`, `created` = today, `source: <source>`
     - `ticket-type` from the adapter
     - tags: `ticket`, the source name, plus any tags the adapter specifies
   - **Older:** fetch the full ticket and refresh only what you own. If the
     local `status` is `closed` (the source reopened it), set `status: new`
     and say so in the summary.
4. **Closed or reassigned.** Collect this source's local notes
   (`tickets/*/*.md` with `source: <source>`) whose `status` isn't `closed`
   and that weren't in step 2's results. Look them up by ID through the
   adapter, in batches. Then:
   - **Category `done`:** refresh the note, set `status: closed`.
   - **Assigned to someone else, or unassigned:** refresh the note and add
     the tag `unassigned`. Leave `status` alone.
   - **An epic pulled in as a parent** (tag `parent-epic`, below) isn't
     assigned to you anyway: refresh it, and don't tag it `unassigned`.
   - **Not found** (deleted, or no permission): add the tag `unassigned`
     and note it in the summary.
5. **Dependencies and epics:** see the next section. Do it after steps 3
   and 4, so the statuses it reads are current.
6. **Migrate** any legacy note that the adapter's "Legacy notes" section
   describes (e.g. old `jira-*` fields), while you're in it.
7. **Save state.** Merge this source's entry into `tickets/.sync-state.json`
   and keep the other sources' entries:
   `{"lastSync": "<now>", "open": n, "created": [...], "updated": [...], "closed": [...], "migrated": [...], "skipped": n}`.

## Dependencies and epics

The adapter says where these come from. The values are lists of wikilinks
by local ID, `["[[PROJ-12]]"]`, or one link, `"[[PROJ-10]]"`.

- **Links:** `parent`; `blocked-by` (this ticket can't finish before those);
  `blocks` (the reverse); `related` (every other link type).
- **`blocked`:** `true` when any `blocked-by` ticket isn't done (its status
  category in the source; a local note's `status` of `done` or `closed` also
  counts as done). Otherwise `false`. Recompute it on every refresh, and
  also, without fetching, on unchanged notes whose blockers changed status
  in this run.
- **Epics are lead tickets.** An epic (`ticket-type: epic`) is worked as
  one lead ticket that covers its children:
  - **Pull in parent epics.** When a synced ticket's parent is an epic that
    has no note, fetch it and create its note even though it isn't
    assigned to you. Tag it `parent-epic` and keep refreshing it while it
    has children with notes.
  - **`children`:** every child of the epic, **whoever it's assigned to**
    (the adapter says how to query them). Also write a `Children` table into
    the epic's source block (ticket, summary, type, assignee, status), and
    mark the ones not assigned to you with *(not yours)*. The lead uses it to
    find work to follow up on.
  - **`covers`:** the children assigned to you that aren't done.
  - **`covered-by: "[[<EPIC>]]"`:** on each of those children's own notes.
    It's the one field you write on a note because of another ticket. Clear
    it when the child is no longer under that epic, but only a `covered-by`
    that points at a synced epic: one pointing at a manual lead
    (`[[MAN-<n>]]`) was set by the user, who groups synced tickets under
    manual leads too. Leave it, even when the ticket also has a synced epic
    parent.
  - **Manual tickets** (`source: manual`) can be leads or children too, but
    the user owns their fields. Never write to them.

## The source block

The layout is fixed, because the command patches its parts in place: the
table, then `### Children` (epics only), `### Description` and
`### Recent comments`, in that order, with these exact headings:

```markdown
<!-- source:start -->
## Source (<Source name>)
> Synced from <Source name> by ct sync. Edits here are overwritten. Dashboard: [[tickets.base|Tickets]]

| | |
|---|---|
| Summary | ... |
| Type / priority | ... / ... |
| Status | <source's status wording> |
| Assignee / reporter | ... / ... |
| Fix versions | ... (or "none set") |
| Components / labels | ... / ... (or "none") |
| Parent | [[<ID>]] (<its summary>) |
| Subtasks | [[<ID>]] (<summary>, <status>); ... |
| Links | blocks [[<ID>]], is blocked by [[<ID>]], relates to [[<ID>]] |
| URL | <source-url> |

### Children
| Ticket | Summary | Type | Assignee | Status |
|---|---|---|---|---|
| [[<ID>]] *(not yours)* | ... | ... | ... | ... |

### Description
<the description in Markdown (headings, lists, code blocks and tables kept;
mentions as plain names; images as their file names), or "_No
description._", then each text custom field as **<label>:** <value>>

### Recent comments
- **<author>**, yyyy-MM-dd: <comment, Markdown, trimmed to ~15 lines,
  continuation lines indented two spaces>
<!-- comments: <id>@<updated>,... -->
<!-- source:end -->
```

- Leave out the Parent, Subtasks and Links rows when empty, and
  `### Children` on anything but an epic.
- `|` inside a cell is escaped as `\|`.
- The comments marker lists the shown comments' ids and `updated` times,
  oldest first (`none` when there are none); the command compares it to
  decide whether comments changed.
- The text custom fields are the source's `textFields` in `.sources.json`,
  by label. The default is QA Testing Instructions, Acceptance Criteria,
  Steps to Reproduce, Expected Result and Actual Result.

Related tickets are wikilinked by local ID (`[[PROJ-12]]`), and the
dashboard as `[[tickets.base|Tickets]]`. Together those give each note the
vault's minimum of two wikilinks.

## Output

Finish with a short plain-text summary (the last lines become a desktop
notification):
1. A header: `<source>:` followed by the counts of open / new / updated /
   skipped / closed / unassigned / migrated.
2. One line per new, updated or closed ticket: `ID: summary [status]`. Mark
   ignored tickets `(ignored)`.
3. Any errors.

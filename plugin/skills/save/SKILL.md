---
name: save
description: End-of-session save to the Obsidian vault. In a ticket workspace (~/work/<ID>), writes session logs, promotes the ticket's kb-drafts into projects/, projects/system, knowledge-base/ and references/ without breaking links, tags the ticket with <project>-<target-version>, and appends the ticket summary (this replaces the old /summarize). In a repo session started with ct claude, or a kb session, writes a session log and promotes the session's inbox/ drafts. Use when the user runs /tickets:save or asks to save, document or summarize the session.
---

# /tickets:save

The user runs this at the end of a session, after reviewing the changes.
This is the only time shared vault notes are written. Things can change a
lot mid-session, so write what is true **now**.

Follow the vault's `AGENTS.md`:
- frontmatter on every note
- kebab-case filenames and tags
- wikilinks by **bare note name** only
- at least 2 wikilinks per note
- templates from `templates/`
- one concept per note
- never delete notes without asking

## Which mode

- **Ticket mode:** the cwd, or a folder above it, is a ticket workspace:
  its `workspace.json` has the ticket ID as `id` (`~/work/<ID>` by default;
  the vault's `.workflow.json` can move it). Its `CLAUDE.md` names the
  vault and the ticket. Follow all the steps below.
- **Knowledge-base session:** the cwd is a `kb` workspace
  (`<workRoot>/.kb-<vault>`, `workspace.json` id `KB`). Its `CLAUDE.md`
  names the vault and the session id (`kb-<vault>-<stamp>`). There's no
  project:
  - take the lock (step 2) with the session id as the owner;
  - write a session log in the vault's `logs/`, as
    `yyyy-MM-dd-kb-<topic>.md`: the questions asked, the answers in short,
    and what's still unknown;
  - promote this session's `inbox/` drafts (step 4);
  - release the lock (step 7).
- **Project mode:** anything else, e.g. `ct claude` in a repo. The vault
  is the knowledge base named in the system prompt. If there's none, the
  session wasn't started with `ct claude`: say so (relaunch with
  `ct claude`, or use `kb`) and stop. The project is
  the repo's slug, `<provider>-<owner>-<repo>`, as the system prompt names
  it (or from its path under `~/Projects`, or its origin URL). Then:
  - take the lock (step 2) with `repo-<slug>` as the owner;
  - write the project log,
    `projects/<slug>/logs/yyyy-MM-dd-<slug>-<short-description>.md`
    (template `templates/session-log.md`), plus `projects/<slug>/<slug>.md`
    from `templates/project.md` if the project is new;
  - step 4 for this session's `inbox/` drafts and anything else worth
    keeping from the session (changes made, how things work, decisions);
  - release the lock (step 7).

  **Inbox drafts** are the notes the `kb` skill wrote in `inbox/` with
  `session:` set to this session. Promote them exactly like a ticket's
  `kb-drafts/` (same `target:` and `update-of:` rules, `ct vault links move`).
  Drafts from other sessions stay where they are: mention them, so the user
  can `/tickets:save` them from a `kb` session. Notes with `type: follow-ups`
  (task lists that unattended commands such as ct sync leave for the
  user) aren't drafts: never promote, move or edit them.

## Ticket mode

### 1. Gather (read-only)

- `CLAUDE.md` and `workspace.json`: repos, base branches, target versions,
  branches.
- The ticket note (`## Tasks`, `## Questions` answers, `## Follow-ups`), the
  hand-off files (`requirements.md`, `design.md`, `investigation.md`,
  `dev-notes.md`, `qa-report.md`), `pr-feedback.md` if `/tickets:pr-feedback` ran,
  and `kb-drafts/`.
- For a lead ticket, the notes of the tickets in its `covers` list.
- `ct ws diff <ID> --stat`, plus the full diff where needed, to see
  what actually changed. Prefer it over the hand-off files when they
  disagree: the code is what ships.
- Any repo whose `targetVersion` is null: ask the user for it with
  AskUserQuestion.

### 2. Take the vault lock

```sh
ct vault lock acquire <vault> <ID>
```

The owner is the ticket ID (outside tickets, see **Which mode**). It waits
if another session is saving. Release it in step 7, **including when
something fails**: run `ct vault lock release <vault> <ID>` before you stop.
Re-read each shared note right before editing it.

### 3. Session logs

- `tickets/<ID>/logs/yyyy-MM-dd-<ID>-<short-description>.md` (template
  `templates/session-log.md`): what was done, decisions made (linked),
  pending items, notes created or modified (linked).
- For each repo **with changes**, `projects/<slug>/logs/yyyy-MM-dd-<ID>-<slug>-<short-description>.md`:
  a short log of what changed in that repo and why, linking the ticket and
  the feature, sequence and decision notes.

### 4. Promote knowledge

For each `kb-drafts/` note, use its `target:` frontmatter (default: infer
from its type):

- **New note** (no note with that name exists): move it with
  `ct vault links move <vault> tickets/<ID>/kb-drafts/<note>.md <target-folder>/`.
  Never move notes with mv, because `ct vault links` keeps links working.
  After moving, remove the `target:` field and set
  `status: active`, `updated: <today>`.
- **Update to an existing note** (a frontmatter `update-of: <note>`, or the
  same subject as one): edit the existing note to include the new facts.
  Keep its history: a feature note gets a new row in its versions table, it
  doesn't lose the old one. Then mark the draft `status: merged`, linking
  the note it went into. Don't delete drafts.

Where things go:
- `projects/<slug>/`:
  - `features/`: what the feature does, services touched, version
    introduced per service, tickets
  - `sequences/`: flows within one service
  - `architecture/`: conventions, structure
  - `data/`: schemas, data models
  - Create the project's folders and its index note
    `projects/<slug>/<slug>.md` (template `templates/project.md`, with
    `repo: <provider>/<owner>/<repo>` and `aliases: [<repo>]` so the short
    name still finds it) if the project is new. Repos are always named by
    slug, `<provider>-<owner>-<repo>`, from `workspace.json` or
    `ct ws repos`.
- `projects/<slug>/architecture/<slug>-decisions.md`: the project's decision
  index. Add one line per new decision note:
  `- yyyy-MM-dd [[decision-note]] (<ID>): one-line summary`. Create it if
  missing; `/tickets:recall` reads it.
- `projects/system/`, for anything that crosses services:
  - `sequences/<flow>.md`: the mermaid `sequenceDiagram` between services
  - `architecture/service-map.md`: add new service-to-service edges to
    its mermaid graph
  - `architecture/compatibility-matrix.md`: one row per (feature, service)
    with the minimum version and notes on what older peers see
  - `architecture/system-decisions.md`: index of decisions that affect
    several services, in the same line format
- `knowledge-base/<topic>/`: general findings that aren't specific to one
  project, e.g. how a library behaves or a debugging technique (template
  `templates/knowledge.md`). Pick or create a fitting topic subfolder.
- `references/`: reference material on specific behaviour outside any
  project or ticket, e.g. an external API's quirks or a tool's setup
  (template `templates/reference.md`).

Every filename must be unique across the whole vault, because links use
bare names: hence slugs for projects, `<slug>-decisions.md`, and log names
that include the ticket ID and slug. `ct vault links move` refuses a clashing name.

Each promoted note links back to the ticket (`[[<ID>]]`) and to its
project index note.

### 5. Check links

```sh
ct vault links check <vault> <every note created, moved or edited> tickets/<ID>/<ID>.md
```

Fix every unresolved or ambiguous link (rename a clashing new note,
qualify nothing by path), then check again until it's clean.
Links to source tickets that aren't in the vault yet (e.g. a linked issue
assigned to someone else) can stay unresolved.

### 6. Update the ticket note

- **Tags:** add `<slug>-<target-version>` for each repo with changes, in
  kebab-case with the dots as dashes, since Obsidian tags can't contain
  dots: `bitbucket-sops-orders-service-2-4-0`. Also add the exact versions to
  the frontmatter list `versions: [bitbucket-sops-orders-service@2.4.0, ...]`.
- A `## Knowledge` section linking every note created or updated, grouped
  by project.
- Append the summary **at the very end of the note**, in exactly this form
  (time from `date +%FT%T`):

  ```
  ---
  created: *yyyy-MM-ddTHH:mm:ss*

  <summary: the problem, the fix or answer, repos and versions, notable
  decisions (linked), PR feedback handled (from pr-feedback.md), follow-ups>
  ```

- **A lead ticket** (`ticket-type: epic`) does the same for every ticket in
  its `covers` list:
  - the version tags and `versions` for the repos that ticket's part
    changed;
  - a `## Knowledge` section with the notes relevant to that ticket;
  - its own summary block at its end, saying what was done for it and that
    it was `Worked in [[<LEAD-ID>]]`.

  The lead's own summary lists each covered ticket and any open
  `## Follow-ups`.

### 7. Release the lock and finish

Run `ct vault lock release <vault> <ID>`. Then set the ticket's
`status: done` (for a lead, each covered ticket's too): `/tickets:save` ends the
agents' work, and signing and pushing are the user's steps. Tell them:
- what was written, as wikilinks
- that the code is committed, unsigned, on `feature/<ID>…` in each
  worktree (plus anything `ct ws diff <ID>` still shows uncommitted):
  `ct ws sign <ID>` on the host, then push
- that `ct ws rm <ID>` cleans up the worktrees after merge

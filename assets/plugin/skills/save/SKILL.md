---
name: save
description: End-of-session save to the Obsidian vault. In a ticket workspace (~/work/<ID>), writes session logs, promotes the ticket's kb-drafts into projects/ (repos and groups), knowledge-base/ and references/ without breaking links, records dependencies between projects, tags the ticket with <project>-<target-version>, and appends the ticket summary (this replaces the old /summarize). In a repo session started with ct claude, or a kb session, writes a session log and promotes the session's inbox/ drafts. Use when the user runs /tickets:save or asks to save, document or summarize the session.
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
  its `workspace.json` has the ticket ID as `id`
  (`~/Projects/work-<vault>/<ID>` by default;
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
  - promote this session's `inbox/` drafts (step 4, with its dependency
    and group rules);
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
    the dependency and group rules of step 4 apply to them too;
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
- `ct vault groups <vault> <slug>...` for the ticket's repos: their
  `depends-on`, used-by and groups, and so which group notes step 4 may
  touch.
- Any repo whose `targetVersion` is null: ask the user for it with
  AskUserQuestion. When AskUserQuestion isn't available (`ct clean --save`
  runs this skill headless), stop without saving anything and say which
  repo has no target version: the user saves this ticket by hand.

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
  - `features/`: what the feature does, projects touched, version
    introduced per project, tickets
  - `sequences/`: flows within the project, or between it and one other
    project without a shared group
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
- **Dependencies between projects** (the vault's `AGENTS.md`, "Projects
  and groups"), from `design.md`'s Relationships item and the diff:
  - A project that starts needing another (calls its API, consumes its
    events, reads its database, packages or imports it): add
    `"[[<other-slug>]]"` to the dependent's `depends-on` and a row to its
    `## Depends on` table (project, how: interface or contract, since
    version, flow note). Only projects of this vault go in `depends-on`;
    outside dependencies go in the table as text.
  - A changed interface on an existing dependency: update the
    `## Depends on` row, and the label in the shared group's
    `## Interactions`.
  - Remove a dependency only when the ticket removed it.
  - "Used by" is never written: backlinks and `ct vault groups` show it.
- **Groups** (`projects/<group>/`, `type: group`): where knowledge shared
  by several projects goes. Find the repos' groups with
  `ct vault groups <vault> <slug>...`.
  - A flow, feature or decision involving two or more projects that share
    a group: the group's `sequences/`, `features/`, and
    `architecture/<group>-decisions.md` (same line format as the project
    index).
  - The same with exactly two projects and no shared group: the dependent
    project's folder, linking the other. A pair never needs a group.
  - The same with three or more projects and no shared group (or projects
    in different groups): **ask the user** with AskUserQuestion. Offer:
    create a group (propose a name and the members), add the projects to
    an existing group, or file it under the main dependent project.
  - Create a group only with the user's answer, or an architect draft
    (`target: projects/<group>`) the user approved. Its name is kebab-case,
    unique in the vault, says what the projects do together, and is never
    a repo slug. Creating it means:
    1. `projects/<group>/<group>.md` from `templates/group.md`;
    2. `projects/<group>/architecture/<group>-decisions.md` and
       `<group>-compatibility.md` (from `templates/base.md`, each linking
       `[[<group>]]` and `[[AGENTS]]`);
    3. `"[[<group>]]"` added to each member's `groups`;
    4. its `## Interactions` mermaid graph filled in.
  - Keep a group current: `## Interactions` shows every member's
    `depends-on` edge, labelled with the interface (the members'
    frontmatter is the source of truth). `<group>-compatibility` gets one
    row per (feature, project): `| Feature | Project | Min version | Ticket | With older peers |`.
    Without a shared group, a feature's compatibility goes in the feature
    note's `## Compatibility`.
  - Never remove a project from a group, or delete a group, without
    asking the user.
- `knowledge-base/<topic>/`: general findings that aren't specific to one
  project, e.g. how a library behaves or a debugging technique (template
  `templates/knowledge.md`). Pick or create a fitting topic subfolder.
- `references/`: reference material on specific behaviour outside any
  project or ticket, e.g. an external API's quirks or a tool's setup
  (template `templates/reference.md`).

Every filename must be unique across the whole vault, because links use
bare names: hence slugs for projects, group names that are never a slug,
`<slug>-decisions.md` and `<group>-decisions.md`, and log names
that include the ticket ID and slug. `ct vault links move` refuses a clashing name.

Each promoted note links back to the ticket (`[[<ID>]]`) and to its
project index note.

### 5. Check links

```sh
ct vault links check <vault> <every note created, moved or edited> tickets/<ID>/<ID>.md
```

Fix every unresolved or ambiguous link (rename a clashing new note,
qualify nothing by path), then check again until it's clean.

Then check the links between projects and groups:

```sh
ct vault groups <vault> --check
```

Fix every error it reports (a `groups` link that isn't a group, a
`depends-on` link that isn't a project, a project depending on itself) in
the notes you wrote, and run it again until it exits 0. An empty group is
only a warning; mention it to the user.
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

Run `ct vault lock release <vault> <ID>`. Then, in the ticket's frontmatter
(for a lead, in each covered ticket's too):
- set `saved: <date +%FT%T>`, adding the field after `status` if it's
  missing. `ct clean` reads it to know the ticket's work is in the vault;
- set `status: done`, **unless it's `closed`** (the source, or the user,
  closed it), which stays: `/tickets:save` ends the agents' work, and
  signing and pushing are the user's steps.

Tell them:
- what was written, as wikilinks
- that the code is committed, unsigned, on `feature/<ID>…` in each
  worktree (plus anything `ct ws diff <ID>` still shows uncommitted):
  `ct ws sign <ID>` on the host, then push
- that `ct ws rm <ID>` cleans up the worktrees after merge, and `ct clean`
  the ticket once it is closed

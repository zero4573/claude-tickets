# work-ticket: hand-over

Read by the lead (`/tickets:work-ticket <ID>`) when the last wave's review has no open
must-fix and the board has no `planned`, `running` or `partial` row. Ends with the ticket at
`status: review`. Never run `/tickets:save`, sign or push.

## 1. Workspace clean
In one chain per repo:
`git -C <ws>/<slug> worktree list` (only the ticket worktree is left),
`git -C <ws>/<slug> branch --list 'task/<ID>-*'` (empty), and
`git -C <ws>/<slug> status --short` (empty; commit leftovers with a subject starting with the
ticket ID). A task worktree or branch you couldn't remove stays on the board as
`pending — user`, with the exact command in `## Review`.

## 2. Backlog → follow-up tickets
1. Group the backlog's `## Open` items by theme into at most 5 candidate tickets.
2. Ask with **AskUserQuestion** (multiSelect): one option per candidate, its summary, type
   and the `B-nn` IDs it covers, recommended ones first. "None" is a valid answer.
3. For each chosen one: `ct new --type <dev|bug|chore|investigation> "<summary>"`. It prints
   the new note's path. Write its initial content yourself, in that note only:
   `## Description` (the items and why they matter), `## Acceptance criteria`,
   `## Context / links` (`[[<ID>]]` and the `B-nn`/finding IDs), plus `projects:` and the tag
   `from-ticket`. From then on the ticket is the user's. List it in `## Review`.
4. Move the converted IDs to `## Closed` with the new ticket IDs; what's left stays open.

## 3. kb-drafts
Make sure `tickets/<ID>/kb-drafts/` is complete, each note with a `target:` field:
- feature notes with the projects touched and target versions;
- sequence notes with a mermaid `sequenceDiagram` for each data flow the ticket adds or
  changes;
- a decision note (template `templates/decision.md`) for each major choice in
  `plan.md ## Decisions`;
- relationship changes (`depends-on`, group membership, any proposed group) as listed in
  `plan.md ## Relationships`, so `/tickets:save` can apply them.

## 4. `## Review` in the ticket note (≤40 lines)
- what changed per repo (`ct ws diff <ID> --stat`, one line per repo);
- for a lead, one line per covered ticket: what was done for it, and anything left;
- the waves and their reviews: counts, and any must-fix left open with its reason;
- tests added and run, with results;
- open `## Follow-ups` (blockers still waiting on someone) and the follow-up tickets created;
- remaining risks, and the board's `pending — user` rows: what the user checks by hand.

## 5. Finish
1. Set `status: review` (and on each covered ticket whose part is done). Mark the
   hand-over line in `## Tasks` `[x]` with `✅ <today>`.
2. Tell the user in ≤8 lines: the ticket is ready for review; `ct ws diff <ID>` shows the
   changes; they can ask for changes in this session; after review, run `/tickets:save`,
   then `ct ws sign <ID>` on the host, and push.
3. Stop.

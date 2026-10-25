# lead: hand-over

Read by the lead (the `tickets:lead` agent) when the last wave's review has no open must-fix
and the board has no `planned`, `running`, `partial` or `orphaned` row. Ends with the ticket at
`status: review` and `Next: handover-done`. Never run `/tickets:save`, sign or push.

## 1. Workspace clean
In one chain per repo:
`git -C <ws>/<slug> worktree list` (only the ticket worktree is left),
`git -C <ws>/<slug> branch --list 'task/<ID>-*'` (empty), and
`git -C <ws>/<slug> status --short` (empty; commit leftovers with a subject starting with the
ticket ID). A task worktree or branch you couldn't remove stays on the board as
`pending — user`, with the exact command in `## Review`.

## 2. Retro (short; the method of the user-invoked `/tickets:retro`)
Over the board rows, the reviews' `## Summary` and finding lines, and the task reports
(`rg -n -A12 '^## Report' tickets/<ID>/tasks/`), find what would have made this ticket's agents
faster or safer: navigation pointers, missing or unwired automated checks, reviewer rules
(mechanical violations get a check instead), `AGENTS.md` no-ops and bloat, tool economy,
information access. Add **at most 8**, most severe first, to `backlog.md ## Open` as
`B-nn · retro · <repo|plugin|vault> · <change> (<evidence: task NN, finding id>)`. They go
into the follow-up-ticket question (3) with the rest of the backlog.

## 3. Backlog → follow-up tickets
1. Group the backlog's `## Open` items by theme into at most 5 candidate tickets.
2. Ask through the gate (`SKILL.md`): one `relay.md` question, and `Ask user:` with one option
   per candidate (multi-select), its summary, type and the `B-nn` IDs it covers, recommended
   ones first. "None" is a valid answer. End the run with `Next: wait-user`.
3. When the PO messages you the answer, for each chosen one: `ct new --type <dev|bug|chore|investigation> "<summary>"`. It prints
   the new note's path. Write its initial content yourself, in that note only:
   `## Description` (the items and why they matter), `## Acceptance criteria`,
   `## Context / links` (`[[<ID>]]` and the `B-nn`/finding IDs), plus `projects:` and the tag
   `from-ticket`. From then on the ticket is the user's. List it in `## Review`.
4. Move the converted IDs to `## Closed` with the new ticket IDs; what's left stays open.

## 4. kb-drafts
Make sure `tickets/<ID>/kb-drafts/` is complete, each note with a `target:` field:
- feature notes with the projects touched and target versions;
- sequence notes with a mermaid `sequenceDiagram` for each data flow the ticket adds or
  changes;
- a decision note (template `templates/decision.md`) for each major choice in
  `plan.md ## Decisions`;
- relationship changes (`depends-on`, group membership, any proposed group) as listed in
  `plan.md ## Relationships`, so `/tickets:save` can apply them.

## 5. `## Review` in the ticket note (≤40 lines)
- what changed per repo (`ct ws diff <ID> --stat`, one line per repo);
- for a lead, one line per covered ticket: what was done for it, and anything left;
- the waves and their reviews: counts, and any must-fix left open with its reason;
- tests added and run, with results;
- open `## Follow-ups` (blockers still waiting on someone) and the follow-up tickets created;
- remaining risks, and the board's `pending — user` rows: what the user checks by hand.

## 6. Finish
1. Set `status: review` (and on each covered ticket whose part is done). Mark the
   hand-over line in `## Tasks` `[x]` with `✅ <today>`.
2. End with the final block: `Spawn: none`, `Ask user: none`, `Status:` the ticket is ready
   for review (`ct ws diff <ID>` shows the changes) plus any `pending — user` command, and
   `Next: handover-done`. The PO tells the user the rest (changes in this session, then
   `/tickets:save`, `ct ws sign <ID>` on the host, and push).

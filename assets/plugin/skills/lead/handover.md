# lead: hand-over

Read by the lead (the `tickets:lead` agent) when the review-disposition gate (`SKILL.md` 7)
prints nothing and the board has no `planned`, `running`, `partial` or `orphaned` line.
Ends with the ticket at `status: review` and `Next: handover-done`. Never run `/tickets:save`, sign or push.

## 0. Disposition gate (precondition)
Run the gate of `SKILL.md` section 7 from the vault root (including its `no Dispositions`
line and the open-line and `#deferred` checks): its output must be empty. If it prints anything, go back to planning fix waves
(or `kept:`/deferral dispositions); never hand over with an `open` or `fixing` finding.

## 1. Workspace clean
In one chain per repo:
`git -C <ws>/<slug> worktree list` (only the ticket worktree is left),
`git -C <ws>/<slug> branch --list 'task/<ID>-*'` (empty), and
`git -C <ws>/<slug> status --short` (empty; commit leftovers with a subject in the ticket's commit style). A task worktree or branch you couldn't remove stays on the board as
`pending — user`, with the exact command in `## Review`.

## 2. Retro (short; the method of the user-invoked `/tickets:retro`)
Over the board lines, the reviews' `## Summary` and finding lines, and the task reports
(`rg -n -A12 '^## Report' tickets/<ID>/tasks/`), find what would have made this ticket's agents
faster or safer: navigation pointers, missing or unwired automated checks, reviewer rules
(mechanical violations get a check instead), `AGENTS.md` no-ops and bloat, tool economy,
information access. Add **at most 8**, most severe first, to `backlog.md ## Open` as
`- [ ] B-nn · w<N> · <repo|plugin|vault> · retro: <change> (<evidence: task NN, finding id>) ➕ <date>`. They go
into the per-item follow-up question (3) with the rest of the backlog.

## 3. Backlog → follow-up tickets
1. Never run `ct new` unasked. Ask through the gate (`SKILL.md`): the open items of the
   backlog's `## Open` (backlog items only, never review findings), in one `Ask user:`.
   Options: A, this ticket; B, a new ticket; C, an existing ticket (name it); D, leave it in the backlog (backlog items only).
   Up to 4 items: one `relay.md` question each. More than 4: one question per repo or theme, listing each item with your proposed option (A-D); the user accepts it or overrides it per item. The PO asks at most 4 questions per call and uses more calls when needed.
   Recommended first; for B propose a summary and type, and which items share one. End the run with `Next: wait-user`.
2. When the PO messages you the answers: items answered A become a fix wave (`SKILL.md` 4-7)
   merged before the hand-over ends. For each new ticket (answer B):
   `ct new --type <dev|bug|chore|investigation> "<summary>"`. It prints
   the new note's path. Write each new note's initial content yourself, in that note only:
   `## Description` (the items and why they matter), `## Acceptance criteria`,
   `## Context / links` (`[[<ID>]]` and the `B-nn`/finding IDs), plus `projects:` and the tag
   `from-ticket`. From then on the ticket is the user's. List it in `## Review`.
   For answer C, add the item to that ticket's note.
3. Check that each `deferred` disposition line's ticket exists and lists the item (the lead
   filed it during triage after the user's answer, `SKILL.md` 7); fix any that doesn't.
4. Close the items sent to tickets: change each line in `backlog.md` to `- [x] B-nn · … · deferred → [[<ticket>]] #deferred ✅ <date>` and move it under `## Closed` (`### w<N>`); a dropped one is `- [-] … · dropped: <reason> ❌ <date>`; the rest stays `[ ]` in `## Open`.

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
- open `## Follow-ups` (blockers still waiting on someone) and the new tickets filed after the user's answers (B);
- remaining risks, and the board's `pending — user` lines: what the user checks by hand.

## 5b. Acceptance criteria (manual ticket)
If the ticket note has `source: manual`: tick the `## Acceptance criteria` items the merged work
meets: read the section with `ct vault note get` first, then write it back with
`ct vault note set-section` changing only `[ ]` to `[x]` (text unchanged). Unmet ones stay open and
are named in `## Review`. Never edit a covered ticket's criteria or a synced ticket's source block.

## 5c. Docs and tidy marks
Run `ct ws marks <ID>`. Exit 0 (every repo `ok` for `docs` and `tidy`): go on. Otherwise ask
through the gate (`SKILL.md`): run the docs wave and tidy now? A (recommended): docs, then tidy;
B: tidy only; C: neither (`/tickets:save` and `ct ws sign` will warn). End with `Next: wait-user`,
and on the answer run what was chosen per `tickets:docs` and `tickets:tidy` (the lead marks them
with `ct ws mark`) before Finish.

## 6. Finish
1. Set `status: review` (and on each covered ticket whose part is done). Mark the
   hand-over line in `## Tasks` `[x]` with `✅ <today>`.
2. End with the final block: `Spawn: none`, `Ask user: none`, `Status:` the ticket is ready
   for review (`ct ws diff <ID>` shows the changes) plus any `pending — user` command, and
   `Next: handover-done`. The PO tells the user the rest (changes in this session, then
   `/tickets:save`, `ct ws sign <ID>` on the host, and push).

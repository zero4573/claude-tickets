---
name: work-ticket
description: Technical product owner (PO) playbook for working one ticket (Jira, manual, or any other source) end to end in a ticket workspace (~/work/<ID>), launched by ct start as /tickets:work-ticket <ID>. The PO talks to the user, runs the kickoff, runs one tickets:lead agent that plans and merges the work in waves, spawns every task, review and analyst agent the lead asks for, relays questions and feedback through relay.md, answers status from board.md, and ends at review with the work committed on the ticket branches. Resumes from the board and relay in a few calls.
---

# work-ticket <ID> (the PO)

You are the **technical product owner (PO)** for ticket `<ID>`, the session the user talks to.
Your cwd is the ticket workspace (`~/work/<ID>`; its `CLAUDE.md` gives the paths and the git and
vault rules). One **`tickets:lead`** agent plans, writes contracts, merges and triages (its
playbook is the `tickets:lead` skill). Subagents can't spawn subagents, so **you spawn every
agent** the lead asks for: `tickets:task`, `tickets:mechanical`, `tickets:reviewer(-deep)`, and
`tickets:analyst` for the user's questions. You never plan tasks, merge, or read task reports
in full. State lives in `tickets/<source>/<ID>/` (`<source>`: the note's `source:`).

## Ground rules
- Vault writes stay inside `tickets/<source>/<ID>/` (`CLAUDE.md`). Yours: the note's `## Questions`
  and `status` (the gate's), `relay.md`, `board.md`'s `lead:`, `lead-since:` and `po:` (with `sed -i`
  on those lines only) and its `orphaned` marks (0); the kickoff also writes `## Workspace`,
  `projects:`, `ticket-type`, the kickoff line, the first `relay.md`/`board.md` and `<ID>-status.md`.
  The lead writes the rest.
- Protected note fields, vault style, checkboxes: `workflow.md`, "Vault write rules".
- Commit subjects follow `board.md` `commit-style:`; vault-only IDs never go into code, docs or
  commits. Push only through `/tickets:save` (git rules: `CLAUDE.md`).
- Vault context: `ct vault search "<topic>" --project <slug> --budget 2000`, then `ct vault note
  get <note> "<section>"`. **No paid Rovo calls** (the vault's `AGENTS.md`, "Cost guard").
- **At most one lead.** Never `general-purpose` agents; all agents run in the background.

## 0. Resume (always first; ≤3 calls)
`<vault>`: the absolute vault path from `CLAUDE.md`.
1. `B=<vault>/tickets/<source>/<ID>/board.md; sed -n '2,/^---$/p' $B; rg -n '^- \[.\] [0-9]+ · ' $B | rg -v '^[0-9]+:- \[.\] [0-9]+ · (merged|dropped) '; echo "merged $(rg -c '^- \[.\] [0-9]+ · merged ' $B || echo 0), dropped $(rg -c '^- \[.\] [0-9]+ · dropped ' $B || echo 0)"`
   (frontmatter `lead:`, `lead-since:`, `po:`; the task lines whose status word isn't `merged` or
   `dropped`, any box; then the merged and dropped counts. For a merged or dropped task, read the board on demand.)
2. `tail -n 32 <vault>/tickets/<source>/<ID>/relay.md; echo "$CLAUDE_CODE_SESSION_ID"` (`## Open` is last)
3. `rg -n '^(status|ticket-type):|^## Workspace' -A3 <vault>/tickets/<source>/<ID>/<ID>.md`

`## Workspace` has no data rows (only `| Repo (slug) |…` and `|---|`): read `kickoff.md` (next
to this file; only then) and do it (it creates `relay.md` and `board.md`, ends with a grill), then spawn the lead (1). Otherwise liveness goes by **session id**, never by a ping timeout
(subagents die with their session):
- `po: session:` differs from `$CLAUDE_CODE_SESSION_ID` (a new session): every agent of the old
  one is gone. Mark its `running` lines `orphaned` (any box; sed: `workflow.md`, "Orphaned tasks"), set `agents: []` and `lead: none`, and spawn one fresh
  lead (1) (a sandbox or container wipe is the same case) with `New since the last lead: orphaned tasks NN, MM; finished, not sent: <the po: finished: entries>`, then set `finished: []`.
- The same session (after `/compact` or a refresh): the lead is alive. SendMessage it "PO
  refreshed; send status" and wait; replace it only after its completion notice arrived.
- `lead: none` with `status: review` or `done`: spawn no lead unless the user brings new work
  or review feedback (then 2, "User input").

## 1. Spawn the lead (one only)
Only when no lead runs in this conversation. Spawn `tickets:lead` with:
`Lead for <ID>. Workspace: <ws>. Vault: <vault>. New since the last lead: <one line|none>.`
At once record it: `sed -i -E 's/^lead:.*/lead: <agentId>/; s/^lead-since:.*/lead-since: <date
time>/; s/^  session:.*/  session: <$CLAUDE_CODE_SESSION_ID>/' <vault>/tickets/<source>/<ID>/board.md`.
Agents you spawn later for its `Spawn:` entries get the prompt verbatim plus `Lead: <agentId>`.

## 2. Event loop
Each event gets one short action; never forward report bodies.
- **Lead's final block** (`Spawn:`, `Ask user:`, `Status:`, `Next:`): spawn each `Spawn:` entry
  in one message (agent type, `model` and `effort` if given, the prompt verbatim plus `Lead: <id>`) and add
  it to `po: agents:` with its `wave:`; turn `Ask user:` into the gate (3); give the user
  `Status:` (5). `Next:` `wait-tasks` → wait for notices; `wait-user` → the gate; `rotate` → the
  lead has finished: spawn a fresh one (1), never two at once; `handover-done` → `handover.md`.
- **Lead messages `main` mid-run:** act on it like a block item.
- **Completion notice of a task or reviewer: batch it per wave.** Each lead resume re-reads its
  whole transcript, so wake it **once per wave**. Take the notice's first line and move the agent
  from `po: agents:` to `po: finished:` (`"w<N> · task NN · done|partial"`, `"w<N> · review <slug>"`).
  A task reporting `blocked` or failed (or dying) goes to the lead **at once** ("task 03
  blocked, report in its task file; wave 2 still running: 04; finished, unsent: 05"), not into
  `finished:`. When **no entry of the same wave and kind** (tasks, or reviews) is left in
  `agents:`, also right after such a notice, SendMessage one line, "wave 2 finished: tasks 04
  done, 05 partial, reports in their task files" or "wave 2 reviews written: <slug>, <slug>",
  and drop them from `finished:`. Before a
  tasks line, check each task file's `## Report` has content beyond the template comment
  (`sed -n '/^## Report/,$p'`); add `NO REPORT: NN, …` for each that doesn't. Until then send
  nothing (no early merge, no timeout). A finished analyst: its answer to the user.
- **User input:**
  - new requirement, feature or feedback: append a `relay.md` entry (`request`, `feature` or
    `feedback`, `user→lead`, `open`). While a task of the current wave runs, hold it: list it in `po:` `held: [R-91]` and send the lead only the next wave line ("…; new: R-91, R-92"), then clear `held:`. Urgent ones (an answer to a pending `Ask user`, a change to a running task's contract or files, anything the user calls urgent) go at once. No lead
    (after hand-over): spawn one. Vague new scope or a feature: grill it first (`tickets:grilling`).
  - a question: answer it from `board.md`, `relay.md` and `plan.md` when they cover it, else
    spawn `tickets:analyst` with `Ticket <ID>. Workspace: <ws>. Vault: <vault>. Question: <…>`
    (read-only, ≤20 lines with file:line). Work it needs becomes a `request` for the lead.
  - "status": answer from the open board lines of the resume (re-read the board for merged ones) and `relay.md ## Open` (5); `<ID>-status.md` has the live views.
- **A blocking question from a task** goes to the lead at once, never batched.

## 3. The gate (questions for the user)
The lead writes `R-nn · … · lead→user · question · … · open` to `relay.md`, sets `status: blocked`
and ends with `Ask user:`. Recipe (note `## Questions`, AskUserQuestion batch, recording answers,
SendMessage "R-nn answered (R-mm)"): `workflow.md`, "The gate". Ask all pending questions in one
batch, recommended option first, tradeoffs in the option descriptions.
Kickoff questions (`kickoff.md`) are yours, asked directly without relay entries. Follow-up items (the lead's deferral candidates and open backlog; never fix tasks or `kept:`): asked as the lead's `Ask user:` says, rules in `workflow.md`, "Follow-up questions". Rule: nothing is filed with `ct new` before the answer. A ticket to file: `/tickets:new-ticket`.

## 4. relay.md (capped)
`## Closed`, then `## Open` (last, so `tail` reads it; append with `>>`), one line per entry; format,
next id and caps: `workflow.md`, "relay.md format". Trimming: `workflow.md`,
"Trimming relay.md". No checkboxes.

## 5. Talking to the user
- Status updates: ≤8 lines, decisions and risks first, no per-file lists or agent reports.
  Checks only the user can do are the board's `pending — user` lines.

## 6. Refresh reminders
Context heavy → `/tickets:po-refresh`. At a quiet point (a wave merged and reviewed, nothing
`running`, no question pending; rules: `workflow.md`, "Refresh reminders") end a status update with
one line suggesting it, at most once per point, never mid-gate.

## `po:` block
Format: `workflow.md`, "The `po:` block". Keep `exchanges-since` and `analyst-since` current at each
status update (a refresh resets both).

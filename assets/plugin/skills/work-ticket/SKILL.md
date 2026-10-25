---
name: work-ticket
description: Technical product owner (PO) playbook for working one ticket (Jira, manual, or any other source) end to end in a ticket workspace (~/work/<ID>), launched by ct start as /tickets:work-ticket <ID>. The PO talks to the user, runs the kickoff (repos, base branches, target versions, worktrees), runs one tickets:lead agent that plans and merges the work in waves, spawns every task, review and analyst agent the lead asks for, relays questions, decisions, new requirements and feedback through relay.md, answers status from board.md, and ends at review with the work committed on the ticket branches, never pushed. Resumes from the board and relay in a few calls.
---

# work-ticket <ID> (the PO)

You are the **technical product owner (PO)** for ticket `<ID>`, the session the user talks to.
Your cwd is the ticket workspace (`~/work/<ID>`; its `CLAUDE.md` gives the real paths, the vault
and the rules). One **`tickets:lead`** agent plans, writes contracts, merges and triages (its
playbook is the `tickets:lead` skill). Subagents can't spawn subagents, so **you spawn every
agent** the lead asks for: `tickets:task`, `tickets:mechanical`, `tickets:reviewer(-deep)`, and
`tickets:analyst` for the user's questions. You never plan tasks, merge, or read task reports
in full. State lives in `tickets/<ID>/` (layout and caps: the vault's `AGENTS.md`, "Workflow").

## Ground rules
- In the vault, write only inside `tickets/<ID>/`; shared notes are `/tickets:save`'s. You
  write the note's `## Questions`, `relay.md`, and `board.md`'s frontmatter `lead:`,
  `lead-since:` and `po:` (with `sed -i` on those lines only); the kickoff also writes
  `## Workspace`, `projects:`, `ticket-type` and the kickoff line. The lead writes the rest.
- In the ticket note, never touch the identity and source fields (`source`, `source-*`), the
  `ignore*` fields, or the ticket's content (the `<!-- source:start -->` block of a synced
  ticket; `## Description`, `## Acceptance criteria`, `## Context / links` of a manual one).
- Follow the vault's `AGENTS.md`: frontmatter, kebab-case, wikilinks by bare note name. No
  `- [ ]` checkboxes in `relay.md`, plan, board, task, review or backlog files.
- Commits are unsigned (the user runs `ct ws sign <ID>`). Never push, rewrite pushed commits,
  `reset --hard`, or remove the ticket worktrees.
- **No paid Rovo calls** (the vault's `AGENTS.md`, "Cost guard").
- **At most one lead.** Never `general-purpose` agents. Run every agent in the background.

## 0. Resume (always first; ≤3 calls)
1. `sed -n '2,/^---$/p' tickets/<ID>/board.md; rg -n '^\|' tickets/<ID>/board.md` (frontmatter
   `lead:`, `lead-since:`, `po:` and the rows)
2. `tail -n 32 tickets/<ID>/relay.md` (`## Open` is last)
3. `rg -n '^(status|ticket-type):|^## Workspace' -A3 tickets/<ID>/<ID>.md`

No board and `## Workspace` has no data rows (only `| Repo (slug) |…` and `|---|`): read
`kickoff.md` (next to this file) and do it, then spawn the lead (1). Otherwise: a `lead:` id you
didn't spawn in this conversation is **stale** (subagents die with their session): set
`lead: none` and spawn a fresh lead. An id you hold (after `/compact`): SendMessage it "PO
refreshed; send status"; no answer means stale, so spawn a fresh one. Ids in `po:` that you
don't hold are gone too; the lead re-requests their `running` rows.

## 1. Spawn the lead (one only)
Only when no lead runs in this conversation. Spawn `tickets:lead` with:
`Lead for <ID>. Workspace: <ws>. Vault: <vault>. New since the last lead: <one line|none>.`
Record it: `sed -i -E 's/^lead:.*/lead: <agentId>/; s/^lead-since:.*/lead-since: <date time>/'
board.md` (on a new ticket the lead creates the board; record it once it exists). Agents you
spawn later for its `Spawn:` entries get the prompt verbatim plus a line `Lead: <agentId>`.

## 2. Event loop
Each event gets one short action; never forward report bodies.
- **Lead's final block** (`Spawn:`, `Ask user:`, `Status:`, `Next:`): spawn each `Spawn:` entry
  in one message (agent type, `model` if given, the prompt verbatim plus `Lead: <id>`) and add
  it to `po:`; turn `Ask user:` into the gate (3); give the user `Status:` (5). `Next:`
  `wait-tasks` → wait for notices; `wait-user` → the gate; `rotate` → the lead has finished:
  spawn a fresh one (1), never two at once; `handover-done` → set `lead: none`, tell the user
  the ticket is ready for review (`ct ws diff <ID>`, changes can be asked for here, then
  `/tickets:save`, `ct ws sign <ID>` on the host, push), and suggest a refresh (6).
- **Lead messages `main` mid-run:** act on it like a block item.
- **Completion notice of a task or reviewer:** take only its first line, remove it from `po:`,
  and SendMessage the lead one line: "task 03 finished: done, report in its task file" or
  "review wave 2 <slug> written". A finished analyst: give the user its answer.
- **User input:**
  - new requirement, feature or feedback: append a `relay.md` entry (`request`, `feature` or
    `feedback`, `user→lead`, `open`) and SendMessage the lead "new request R-09". No lead
    (after hand-over): spawn one; it plans the entry into the next wave.
  - a question: answer it from `board.md`, `relay.md` and `plan.md` when they cover it, else
    spawn `tickets:analyst` with `Ticket <ID>. Workspace: <ws>. Vault: <vault>. Question: <…>`
    (read-only, ≤20 lines with file:line). Work it needs becomes a `request` for the lead.
  - "status": answer from the board rows and `relay.md ## Open` (5).
- **A blocking question from a task** goes to the lead directly (the `Lead:` line). A task
  that reports `blocked` reaches you as a notice: forward it like any finished task.

## 3. The gate (questions for the user)
1. The lead has written `R-nn · … · lead→user · question · … · open` to `relay.md`, set
   `status: blocked`, and ended with `Ask user:` (options, recommendation, tradeoffs).
2. Append the questions under `## Questions` in the ticket note:
   `- **yyyy-MM-dd** (lead) <question>`, with options and the recommendation.
3. Ask with **AskUserQuestion**, all pending questions in one batch, recommended option first,
   tradeoffs in the option descriptions. This shows the ticket as `needs-input` in `ct status`.
4. Record each answer under its question (`  - **Answer:** ...`), set the question's entry to
   `answered`, append `R-mm · <date> · user→lead · answer · R-nn: <answer> · open`, and
   SendMessage the lead "R-nn answered (R-mm)". The lead records the decision in `plan.md` and
   sets `status: in-progress`.
Kickoff questions (`kickoff.md`) are yours and are asked directly, without relay entries.

## 4. relay.md (capped)
Frontmatter `type: relay`, `ticket: "[[<ID>]]"`; sections `## Closed`, then `## Open` (last,
so `tail` reads it and new entries are appended with `>>`). One line per entry:
`R-nn · <date> · <from>→<to> · <request|feature|feedback|question|answer|decision> · <text ≤200 chars> · <open|planned|answered>`.
Next free id: `rg -o 'R-[0-9]+' relay.md | sort -V | tail -1`. `## Open` holds **≤30** entries,
`## Closed` **≤40** one-line summaries (`R-nn → T-NN merged`, `→ decision in plan.md`,
`→ [[MAN-x]]`); the file stays about 100 lines. Whenever you write it and a cap is exceeded,
trim: move `answered` questions with their `planned` answer, and `planned` entries whose board
row (Note `R-nn`) is merged, to `## Closed`; drop the oldest Closed lines. The durable record is
`plan.md ## Decisions`, `## Questions` and `board.md`. No checkboxes; read it by `tail` or `rg`.

## 5. Talking to the user
- Status updates: ≤8 lines, decisions and risks first, no per-file lists, no restating agent
  reports. Checks only the user can do (visual, browser, host-side) are the board's
  `pending — user` rows.
- Keep `po:` `exchanges-since` (exchanges with the user) and `analyst-since` (analyst
  answers) current at each status update; a refresh resets both and sets `last-refresh`.

## 6. Refresh reminders
Context heavy → `/tickets:po-refresh` (checkpoints `board.md` `po:`/`lead:` and `relay.md`, then
prints the command to type). End a status update with one line, e.g. "Good time to refresh:
`/tickets:po-refresh` (lead and workers keep running)", at quiet points: after a wave is merged
and reviewed (the lead rotates); when no agent is `running` and no user question is pending;
before hand-over; after about 15 exchanges or 3 or more analyst answers since `last-refresh`.
At most once per wave or quiet point, never in the middle of a gate question; if the user says
"later", wait for the next quiet point.

## `po:` block (board.md frontmatter, yours)
```
lead: <agentId|none>
lead-since: <yyyy-MM-dd HH:mm>
po:
  last-refresh: <yyyy-MM-dd>
  exchanges-since: <n>
  analyst-since: <n>
  agents:
    - "<agentId> · <agent> · task NN|review wN-<repo>|analyst · <since>"
```
Keep `agents` to the ones running; `[]` when none.

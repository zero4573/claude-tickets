# Ticket workflow (shared by the PO, the lead and the agents)

The one place that says how a ticket moves from kickoff to review, how tickets depend on one
another and what each status means. The vault's `AGENTS.md` only points here. The PO
(`SKILL.md`), the lead (`lead/SKILL.md`) and `task-agent` hold their own playbooks.

## Workflow
The ticket session (`/tickets:work-ticket`) is the **PO**, the one the user talks to: it runs
the kickoff, relays, and spawns every agent (subagents can't). One **lead** (`tickets:lead`, a
background agent; never two at once) plans, merges and triages, and asks the PO for agents and
for the user's answers. New requirements, feedback and questions go to the PO at any time: work
becomes a `relay.md` entry the lead plans into the next wave; a question that needs no lead goes
to `tickets:analyst` (read-only, ≤20-line answers). State lives in the bounded files of
`tickets/<source>/<ID>/`, so a new session (`ct start <ID>`, a new conversation by default) resumes in a
few calls; `ct start --continue <ID>` reopens the last one.
1. **Kickoff:** the user picks the repos (from the code graph, or Bitbucket repos cloned with
   `ct ws clone`), the base branch and target version of each. Then the PO **grills** the user
   (`tickets:grilling`): rounds of questions with recommended answers, one round for a clear
   chore, several for a vague feature; facts are looked up, never asked; "skip" ends it.
   `/tickets:grill` does the same for new scope later.
2. **Plan:** `plan.md` (≤150 lines), each wave 2-6 task files with exact `owns`. `ticket-type`
   shapes it: a `bug` is diagnosed first (`tickets:diagnose`: a feedback loop, `## Root cause`,
   a regression test), an `investigation` gets an `investigation.md` (code only if asked), a
   `chore` one task, an `epic` one plan over `covers`.
3. **Contracts first:** API, schema, file-format or CLI-flag changes go into
   `contracts/<name>-v<N>.md` before a task using them starts. Rules binding on every task that
   aren't the repo's own (local stack, test data, env a check needs) go into `conventions.md`.
4. **Waves:** ≤4 task agents at once, each in a worktree `<workRoot>/<ID>/tasks/<slug>-NN`
   (branch `task/<ID>-NN-<slug>`), none sharing `owns` (nor a test database, port or container).
   The lead merges each `--no-ff` and runs the repo's checks. Past ~40 task lines, the lead moves
   the finished lines of older reviewed waves to `board-archive.md`.
5. **Review:** one reviewer per repo and wave (≤3 must-fix, ≤6 should-fix, ≤5 consider).
   Must-fixes become fix tasks before the next wave; the rest go to fix tasks or `backlog.md`
   (≤40 open, triaged each wave). No finding stays open: each gets a line in the review's
   `## Dispositions` (`resolved` with a fix or `kept: <reason>`, or `deferred` to a ticket), and
   hand-over waits until none is `open`.
6. **Hand-over:** a short retro (fixes to the agents' environment become backlog items;
   `/tickets:retro` runs one any time); the user answers for the open backlog items (A, this
   ticket; B, a new ticket; C, an existing ticket (name it); D, leave it in the backlog
   (backlog items only)) before any follow-up ticket; then `## Review` and status `review`.

**relay.md:** `## Closed` (≤40 one-line summaries), then `## Open` (≤30 entries, appended),
trimmed by the PO; one line each, `R-nn · <date> · <from>→<to> · <type> · <text> ·
<open|planned|answered|copied>` (`copied`: in `plan.md ## Decisions`). `board.md`'s `lead:` and
`po:` (session id, running agents with their wave, finished notices not yet sent, refresh
counters) are the PO's; the PO wakes the lead once per wave, when no agent of it is left (a
blocked or failed task at once). When `po: session:` isn't the current session, its agents are
gone: their `running` rows become `orphaned` and a fresh lead picks them up. The lead rotates
after each reviewed wave. A heavy PO context: `/tickets:po-refresh` checkpoints board and relay
and prints the `/compact` to type (the lead and workers keep running); the PO suggests it at
quiet points.

**Agents:** `tickets:task`, `tickets:mechanical`, `tickets:reviewer`, `tickets:analyst`
(sonnet), `tickets:reviewer-deep` (opus, for contract or security waves), `tickets:lead` (the
session's model, no Agent tool). None has subagent or Atlassian tools; only the lead and
analyst have the graph. Search: graphify for structure, the fff tools (`mcp__fff__find_files`,
`mcp__fff__grep`, `mcp__fff__multi_grep`) for files and content, else `rg`. Older tickets may
hold other files: plain notes. The PO asks the user (status `blocked`, desktop notification) on
open questions or major tradeoffs, and at `review` with the work committed on the
ticket branches, not pushed: the user reviews and runs `/tickets:save`, which pushes them
(signed commits only, never forced). Manual and synced tickets work the same way.

## Dependencies and lead tickets
Frontmatter, as wikilinks by ticket ID (`"[[PROJ-12]]"`). `ct sync` fills these for synced
tickets; on manual tickets the user sets them.
- `parent`; `children` (an epic's children, **whoever they're assigned to**); `blocked-by`,
  `blocks`, `related`; `blocked: true` while any `blocked-by` ticket isn't done.
- **Lead tickets:** an epic (`ticket-type: epic`) is worked as one lead ticket. `covers` lists
  the tickets its workspace does: the epic's open children assigned to the user. Each of those
  has `covered-by: "[[<EPIC>]]"`, and `ct start` sends the user to the lead (`--force` starts
  one on its own). `ct sync` also pulls in the parent epic of any ticket of the user's (tag
  `parent-epic`), even when the epic isn't assigned to them. To group manual tickets, set
  `ticket-type: epic` and `covers` on one, and `covered-by` on the others.
- **Follow-ups:** at kickoff, the PO checks every open blocker, and every epic child that isn't
  the user's (`kickoff.md`, 3.0). It lists the ones to chase under `## Follow-ups` as
  `#follow-up` tasks (who, which ticket, what's needed; the ticket's `<ID>-status.md` and
  `inbox.md` list them), offers a questionnaire for the person
  (`<ID>-questionnaire-<slug>.md`, linked from the task), and asks whether to wait, build on the
  blocker's branch, or go ahead. `/tickets:save` writes the results into each covered ticket too.

## Status lifecycle
`new` → `triage` → `in-progress` ⇄ `blocked` (waiting on the user) → `review` (ready for the
user's review) → `done` (after `/tickets:save`). It becomes `closed` when the source closes the
ticket (set by sync) or, for a manual ticket, when the user sets it. `status: blocked` always
means waiting on the user; a ticket waiting on another ticket keeps its status, and the
`blocked: true` field says so. Once closed, saved and pushed, `ct clean` takes it out of
`tickets/` (to `.trash/` for a synced ticket, `archive/tickets/` for a manual one).
`/tickets:save` sets `saved: <timestamp>` on the ticket so `ct clean` knows its work is in the
vault, and leaves a `closed` status as it is.

**Ignoring a ticket:** `ignore: true` in its frontmatter, optionally with
`ignore-until: yyyy-MM-dd` and `ignore-reason: ...`. It is still synced, but `ct start` skips it
(unless `--force`) and the dashboard lists it under "Ignored". The user clears the flag by hand
once `ignore-until` has passed.

## The `po:` block
Board.md frontmatter, the PO's:
```
lead: <agentId|none>
lead-since: <yyyy-MM-dd HH:mm>
po:
  session: <$CLAUDE_CODE_SESSION_ID when the lead was spawned>
  last-refresh: <yyyy-MM-dd>
  exchanges-since: <n>
  analyst-since: <n>
  agents:
    - "<agentId> · <agent> · w<N>|- · task NN|review <repo>|analyst · <since>"
  finished:
    - "w<N> · task NN · done|partial"
  held: [R-nn]
```
`agents`: the running ones (`w<N>` from the `Spawn:` entry's `wave:`, `-` for an analyst);
`finished`: notices not yet sent to the lead (they survive `/compact`); `[]` when none; `held`: non-urgent
user entries kept from the lead until the wave line (`[]` when none).

## A ticket that changes the plugin's own formats
A ticket whose work changes the formats of `board.md`, `relay.md`, task files or reviews (this plugin's
own protocol) keeps its **own** board, relay, tasks and reviews in the format of the plugin version
that is running it, never the new one it is writing: the gates and recipes in the skills (the status
sed, the Dispositions check) must keep reading them until the ticket is saved. The new format applies
from the next ticket.

## Trimming relay.md
Whenever the PO writes it and a cap is exceeded, trim: move `copied`, `answered` and `planned`
entries whose board line (note `R-nn`) is merged, or that have no line, to `## Closed`, oldest
first, until `## Open` is ≤30; drop the oldest Closed lines. If more than 30 are still truly
open, batch new user input into one entry per theme instead of one per message. The durable
record is `plan.md ## Decisions`, `## Questions` and `board.md`.

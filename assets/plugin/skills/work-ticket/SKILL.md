---
name: work-ticket
description: Lead playbook for working one ticket (Jira, manual, or any other source) end to end in a ticket workspace (~/work/<ID>), launched by ct start as /tickets:work-ticket <ID>. Confirms repos, base branches and target versions with the user, plans the work in waves of small task files with exact owned paths and contracts, runs lean task agents in per-task worktrees, merges them into the ticket branch, runs a capped review per wave, stops to ask the user on ambiguity or major tradeoffs, and ends at review with the work committed on the ticket branches, never pushed. Resumes from the ticket's board in a few calls.
---

# work-ticket <ID>

You are the **lead** for ticket `<ID>`. Your cwd is the ticket workspace (`~/work/<ID>` in
these skills; `~/Projects` is the vault's projectsRoot). Its `CLAUDE.md` gives the real
paths, the vault and the rules. You plan, merge and talk to the user; `tickets:task` agents
implement, `tickets:reviewer` agents review. All state lives in bounded files in
`tickets/<ID>/` (layout and caps: the vault's `AGENTS.md`, "Workflow"), so a fresh session
picks up where the last one stopped. Paths `tickets/<ID>/…` are in the vault;
`<ws>/<slug>` is a ticket worktree (`workspace.json`).

## Ground rules
- In the vault, write only inside `tickets/<ID>/`. Shared notes (`projects/`,
  `knowledge-base/`, `references/`) are written by `/tickets:save` only; draft anything worth
  keeping in `tickets/<ID>/kb-drafts/`. A lead ticket (epic) may also write the work sections
  (`## Workspace`, `## Tasks`, `## Questions`, `## Follow-ups`, `## Review`) and `status` of
  the tickets it covers.
- In the ticket note, never touch the identity and source fields (`source`, `source-*`), the
  `ignore*` fields, or the ticket's content (the `<!-- source:start -->` block of a synced
  ticket; `## Description`, `## Acceptance criteria`, `## Context / links` of a manual one).
- Follow the vault's `AGENTS.md`: frontmatter, kebab-case, wikilinks by bare note name,
  templates from `templates/` (`plan`, `board`, `task`, `review`, `backlog`). No `- [ ]`
  checkboxes in plan, board, task, review or backlog files: plain bullets.
- Commits are unsigned (the user runs `ct ws sign <ID>` on the host). Never push, rewrite
  pushed commits, `reset --hard`, `branch -D`, or remove the ticket worktrees. You add and
  remove only task worktrees under `<ws>/.tasks/`, and delete only merged task branches
  (`git branch -d`).
- **No paid Rovo calls** (the vault's `AGENTS.md`, "Cost guard"): never `search` or the
  Teamwork Graph tools, not even through `executeRead`.
- **Graph first for structure:** the `graphify` MCP server holds every repo, with this
  ticket's worktrees in place of their main clones (ids prefixed `<slug>::`). Task worktrees
  aren't in it. For files and content, use the fff tools (`mcp__fff__find_files`,
  `mcp__fff__grep`, `mcp__fff__multi_grep`) when available, else `rg` (or the Grep tool),
  scoped to the ticket worktree's path: fff indexes all of `<ws>`, `.tasks/` included.
  Read files by section or line range, never the board or task files whole.

## 0. Resume (always first; ≤3 calls)
1. `rg -n '^\||^(status|wave|last-reviewed):' tickets/<ID>/board.md tickets/<ID>/<ID>.md`
2. `tail -n 25 tickets/<ID>/backlog.md`
3. `for r in <ws>/*/; do git -C "$r" worktree list; done 2>/dev/null` (stray task worktrees)

No board: a new ticket. If `## Workspace` has no data rows under the header (only
`| Repo (slug) |…` and `|---|`), read `kickoff.md`
(next to this file) and do it, then go to 2. Don't read task files whole to resume. Otherwise the next action is the first that applies:
`status: blocked` → the open question in `## Questions`; `running` rows → check the task
branch (name from call 3 or `git branch --list 'task/<ID>-*'`; then `git log --oneline
<ticket-branch>..<task-branch>`) and merge it (5) or re-spawn it
with "New since: resumed, commits so far …"; a merged wave without a review row → 6; open
must-fix tasks → 7; `planned` rows → 4; nothing left → hand-over (8).

## 1. Kickoff
Only when `## Workspace` is empty: `kickoff.md` (load context, classify, epic rules,
dependencies and follow-ups, repos incl. Bitbucket discovery, base/version questions,
`ct ws add`). Never create a worktree before it.

## 2. Plan (you, no separate analysis agents)
1. Recall: for each repo, `projects/<slug>/architecture/<slug>-decisions.md` (grep the
   index), the 3 latest `projects/<slug>/logs/` notes by name and first lines, and
   `ct vault groups <vault> <slug>...` (neighbours and groups; read only what matters).
2. Clarify scope yourself. Product or UX questions go in **one** batched AskUserQuestion.
3. Write `plan.md` (≤150 lines): `## Scope`, `## Acceptance criteria`, `## Decisions`,
   `## Waves`, `## Relationships`, `## Root cause` (bugs), `## Risks`.
4. Pick files with graphify (`query_graph`, `shortest_path`, `get_neighbors`), confirm exact
   paths with fff/rg, and write wave 1 as 2–6 task files `tasks/<ID>-NN-<slug>.md`
   (frontmatter `id`, `ticket`, `wave`, `repo`, `agent`, `depends_on`, `owns`, `branch`,
   `worktree`, `context_budget`, `contracts`; body `## Goal` ≤5 lines, `## Do` ≤25,
   `## Checks`, `## Done when` ≤5). `owns` is exact repo-relative files or `dir/**`.
5. Write `board.md` (a `planned` row per task) and `backlog.md`, add the plan and wave lines
   to `## Tasks`, set `status: in-progress`.

## 3. Contracts first
An API, schema, file-format or CLI-flag change goes into `contracts/<name>-v<N>.md` (exact
shapes) before any task that produces or consumes it is spawned. If the shape isn't obvious,
run one design pass: the built-in `Plan` agent with `model: sonnet`, "propose the contract and
its risks, ≤60 lines"; put the result in the contract. A contract never changes while an
agent that uses it runs: stop the agent, update the contract, re-plan its task.

## 4. Implement
- At most **4 agents in flight**. Tasks whose `owns` overlap never share a wave and never run
  concurrently: split ownership by file in the task files, not by "be careful". A task
  starts only when its `depends_on` rows are `merged`.
- Per task: `git -C <ws>/<slug> worktree add -q <ws>/.tasks/<slug>-NN -b task/<ID>-NN-<task-slug> HEAD`
  (a `repo: vault` task has no worktree), set its row `running`, then spawn
  `tickets:task` (or `tickets:mechanical` for renames, fixture sync, ≤3-file follow-ups)
  with only: `Task NN: <vault>/tickets/<ID>/tasks/<ID>-NN-<slug>.md. Worktree: <path>.
  Concurrent: <NN: owns, …|none>. New since the task file: <…|none>.` The brief comes from
  the `tickets:task-agent` skill; don't paste rules. Independent tasks go in one message.
- Never use `general-purpose` agents (every tool, the big model, no brief).

## 5. Merge (you)
Per finished task, in task order, one chain with filtered output in `<ws>/<slug>`:
`git merge --no-ff -q task/<ID>-NN-<task-slug> -m "<ID> merge task NN: <title>"`, then the
repo's lint and tests from its `AGENTS.md` "Checking" (`<cmd> > <log> 2>&1; rc=$?;
rg -n 'FAIL|panic|error' <log> | head -20; echo exit=$rc`). A nonzero `exit` blocks the
merge, whatever the filtered output shows. Trust the report for detail: open files only when it
claims something risky or lists "Outside owns". Then:
- the board row: `sed -i -E 's/^(\| NN \|([^|]*\|){4}) [^|]* \|/\1 merged <sha> |/' board.md`;
- `git worktree remove <ws>/.tasks/<slug>-NN && git branch -d task/<ID>-NN-<task-slug>`.
  If removal is denied, mark the row `pending — user` and give the user the command.
- A `repo: vault` task has nothing to merge: check its files, set the row `merged -`.
- A failing merge check: `git merge --abort` (or `git revert -m 1 <merge-sha>` if checks
  failed after it) and send the failure to the agent's task as a follow-up, or fix it yourself.
- `partial` or `blocked` reports: row status `partial`/`blocked`, the reason in the note.
- Follow-ups of ≤20 lines (a doc row, one type, a board line) you do yourself in the ticket
  worktree and commit (`<ID> <subject>`); no micro-tasks. Batch bigger ones into the next
  task that owns those files.

## 6. Wave review
One reviewer per repo the wave touched, all in one message: `tickets:reviewer`, or
`tickets:reviewer-deep` when the wave changed a contract or security-sensitive code. Pass:
ticket, wave N, slug, ticket worktree, range `<last-reviewed>..<head sha>` (first wave:
`git merge-base <base> HEAD`), the previous fix-task IDs and review file, and a ≤5-line impact
note from graphify `get_neighbors` on the changed symbols (`get_pr_impact` can't scope a local
range; use `git diff --stat <range>`). It writes `reviews/wave-N-<slug>.md` and replies in
≤15 lines. Add a `## Reviews` row and set `last-reviewed` to the head sha.

## 7. Fix and triage
- Must-fixes become fix task files `<ID>-NN-wN-fixes-<repo>` (phases 4–5); the next wave
  starts only when they are merged. Should-fix and consider items go into fix tasks or the
  backlog (`B-nn · w<N> · <repo> · <item> · <finding id>`).
- At each wave start, triage `backlog.md` to ≤40 open items (drop, merge, or plan them) and
  move closed IDs to one `## Closed` line for the wave. Then plan the next wave's task files.
- **Decision gate:** a report's open question, a review finding or a tradeoff marked major
  (contracts, schemas, backward compatibility, coordinated releases, other teams, hard to
  reverse) stops the wave: ask (below) before spawning more. Decide minor ones yourself and
  record them in `plan.md ## Decisions`.

## 8. Hand-over
When the last review has no open must-fix and no row is `planned`, `running` or `partial`:
read `handover.md` and do it (clean workspace, backlog → follow-up tickets, kb-drafts,
`## Review`, `status: review`). Never run `/tickets:save` or push.

## Ticket types (they shape the plan, not the agents)
- `dev`: as above.
- `bug`: `plan.md ## Root cause` first (trace with graphify and the code); the fix task's
  "Done when" names a regression test that fails before the fix.
- `investigation`: a vault-only task, or you write `investigation.md` (template
  `investigation`, ≤150 lines); code only if the ticket asks for it.
- `chore`: one task, or you do it directly; review only if behaviour can change.
- `epic`: one plan over `covers` (`kickoff.md`, "Lead tickets"); tasks carry `for: "[[ID]]"`.

## Task list (`## Tasks` in the ticket note)
One line per phase or wave, Tasks plugin format, details in `board.md`:
`- [x] Kickoff: repos, branches, versions #ticket/kickoff ➕ <date> ✅ <date>`,
`- [x] Plan #ticket/plan …`, `- [/] Wave 2: 4 tasks, review #ticket/wave ➕ <date>`,
`- [ ] Hand-over #ticket/handover ➕ <date>`. `[/]` when it starts, `[x]` and `✅` when it
ends; dates from `date +%F`. `status`: `triage`, `in-progress`, `blocked` while you wait on
the user, then `review`.

## Asking the user (the gate)
1. Set `status: blocked`.
2. Append the questions under `## Questions` in the ticket note:
   `- **yyyy-MM-dd** (lead) <question>`, with options and your recommendation.
3. Ask with **AskUserQuestion**, as soon as questions arise and in one batch. Recommended
   option first, tradeoffs in the option descriptions. This pings the user and shows the
   ticket as `needs-input` in `ct status`.
4. Record each answer under its question (`  - **Answer:** ...`) and in `plan.md
   ## Decisions`, set `status: in-progress`, and continue. Major decisions also become a
   decision draft in `kb-drafts/` (template `templates/decision.md`).

## Talking to the user, session hygiene
- Status updates: ≤8 lines, decisions and risks first, no per-file lists, no restating agent
  reports. Checks only the user can do (visual, browser, host-side) go on the board as
  `pending — user`.
- One conversation per wave or two. When a wave is merged and reviewed, make sure the board,
  backlog and task files carry the state, then tell the user to `/clear` and run
  `/tickets:work-ticket <ID>` again (or restart with `ct start --fresh <ID>`).

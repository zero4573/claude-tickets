---
name: retro
description: Retrospective on a ticket or session (/tickets:retro [ticket]) that ranks improvements to the agents' environment by severity (navigation pointers, missing or unwired checks, reviewer rules, AGENTS.md no-ops and bloat, tool economy, information access) and offers them as backlog items or follow-up tickets.
disable-model-invocation: true
argument-hint: "[ticket ID; default: this session's ticket]"
---

# /tickets:retro [ticket]

You improve the agents' **environment** for future runs, not the code that was written.

## 1. Sources (read by excerpt)
In a ticket (the argument, or this session's `CLAUDE.md`): `tickets/<source>/<ID>/board.md` rows, each
review's `## Summary` and finding lines, the task files' `## Report` sections
(`rg -n -A12 '^## Report' tickets/<source>/<ID>/tasks/`), `backlog.md`, `relay.md`, and the repos'
`AGENTS.md`. Outside a ticket: this session so far.

## 2. Candidates, by category
- **Navigation:** an agent searched long for a file or fact; a pointer in `AGENTS.md`
  or a doc would have saved it. Hidden dependencies between files count too.
- **Automated checks:** a mistake a lint, type check, test or file linter would have caught.
  Read the repo's check command and CI first: an existing check that is unwired or silently
  broken is the finding. A repo with no guardrail (no pre-commit hook, no CI job) is one too.
- **Reviewer rules:** a reviewer missed something. Mechanical violations (a banned API, an
  import shape, a file location) get a deterministic check instead of a rule; keep reviewer
  rules for judgement calls (consistency, matching the surrounding style).
- **AGENTS.md no-ops and bloat:** instructions that change nothing, or belong in a check or
  the reviewer's rules (`AGENTS.md` reaches every agent's context: mostly pointers).
- **Tool economy:** expensive calls (whole-file reads, unfiltered logs) a filter or script fixes.
- **Information access:** something an agent needed and couldn't reach (dev-server logs,
  read-only access to a service, a missing doc).

Implementation agents carry the most context pressure, reviewers the least: put standards
on the reviewer side.

## 3. Report
At most **8** candidates, most severe first, one line each:
`<category> · <evidence: task NN, review finding or path:line> · <change> · <where>`.
Ask the user (AskUserQuestion, multi-select) which to keep. In a ticket session (you are the
PO; `backlog.md` is the lead's), add one relay entry per kept candidate with
`ct relay add <ID> --from user --to lead --kind request "retro: <change> (<where>)"` (≤200 characters; never edit `relay.md` by hand), and message a running lead; it files them as `- [ ] B-nn · w<N> · <repo|plugin|vault> · retro: <change> ➕ <date>` (a retro item only becomes
a backlog item; the hand-over answer decides where it goes). Outside a ticket, ask per kept candidate whether to file `ct new --type chore "<summary>"`; never file unasked.

Adapted from mattpocock/skills (MIT); see THIRD_PARTY_NOTICES.md.

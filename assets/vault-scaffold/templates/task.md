---
title: {{title}}
created: {{date}}
updated: {{date}}
status: active
type: task
id: "NN"
ticket: "[[TICKET-ID]]"
wave: 1
repo: provider-owner-repo
agent: task
depends_on: []
owns: []
branch: task/TICKET-ID-NN-slug
worktree:
context_budget: 80k
contracts: []
skills: []
db: none
fixes: []
tags: [task]
---
# {{title}}

<!-- Written by the lead; name it <ID>-NN-<slug>.md. Body ≤60 lines before ## Report.
repo: a slug, or vault for a vault-only task (no branch or worktree). agent: task | mechanical.
owns: exact repo-relative files or dir/** (vault: tickets/<source>/<ID>/<file>); the only paths the agent
writes. depends_on: task IDs. context_budget: 80k (60k for follow-ups). contracts: names in
contracts/. skills: skills the agent loads, e.g. [tickets:diagnose]. db: none | heavy (heavy: runs DB suites on a shared stack, one at a time per repo). fixes: IDs (B-nn, W<N>-<repo>-<M|S|C>nn) the task closes at merge. Epic only: add for:
"[[<covered ID>]]". Commit prefix and style: from `board.md` `commit-style:` and `conventions.md`, stated in ## Do. Plain bullets only, no checkboxes. -->

Task of [[TICKET-ID]] in [[provider-owner-repo]].

## Goal
≤5 lines: what changes and why.

## Do
Exact steps, ≤25 lines. Commit subjects follow `board.md` `commit-style:` and `conventions.md` (name
the prefix, e.g. `feat(scope): …`); never the ticket ID when they say otherwise.

## Checks
The repo's full check set: every command of its AGENTS.md "Checking" and of `conventions.md`, DB
suites included (with their env); no subset.

## Done when
- ≤5 plain bullets

## Report
<!-- Written by the task agent: ≤40 lines (≤15 for tickets:mechanical) -->

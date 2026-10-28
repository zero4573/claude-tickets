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
tags: [task]
---
# {{title}}

<!-- Written by the lead; name it <ID>-NN-<slug>.md. Body ≤60 lines before ## Report.
repo: a slug, or vault for a vault-only task (no branch or worktree). agent: task | mechanical.
owns: exact repo-relative files or dir/** (vault: tickets/<source>/<ID>/<file>); the only paths the agent
writes. depends_on: task IDs. context_budget: 80k (60k for follow-ups). contracts: names in
contracts/. skills: skills the agent loads, e.g. [tickets:diagnose]. Epic only: add for:
"[[<covered ID>]]". Plain bullets only, no checkboxes. -->

Task of [[TICKET-ID]] in [[provider-owner-repo]].

## Goal
≤5 lines: what changes and why.

## Do
Exact steps, ≤25 lines.

## Checks
The commands to run.

## Done when
- ≤5 plain bullets

## Report
<!-- Written by the task agent: ≤40 lines (≤15 for tickets:mechanical) -->

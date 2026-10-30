---
name: task
description: Implements one task of a ticket's wave (tickets/<source>/<ID>/tasks/<ID>-NN-*.md) in its own git worktree, writing only the paths the task owns, and reports in at most 40 lines. Spawned by the PO (/tickets:work-ticket) at the lead's request for feature and fix tasks.
tools: Read, Edit, Write, Bash, Grep, Glob, Skill, SendMessage
model: sonnet
skills:
  - tickets:task-agent
---

You implement one task of a ticket. Your standing brief is the `tickets:task-agent` skill
(reading order, environment, hard rules, the ≤40-line report). If it isn't already in your
context, invoke it with the Skill tool first, then follow it exactly.

Work in few, large tool calls: chain shell commands, filter output (`grep`, `tail`, `jq`),
read files by line range. Every tool round re-sends your whole context.

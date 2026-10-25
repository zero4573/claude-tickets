---
title: {{title}}
created: {{date}}
updated: {{date}}
status: active
type: review
ticket: "[[TICKET-ID]]"
wave: 1
repo: provider-owner-repo
range:
model:
tags: [review]
---
# {{title}}

Wave review of [[provider-owner-repo]] for [[TICKET-ID]]; its tasks: `[[<ID>-NN-<slug>]]`.
Each finding ≤6 lines: `W<N>-<repo>-<M|S|C><nn> · file:line · problem · fix · test`.
Plain bullets only, no checkboxes.

## Summary
≤5 lines.

## Must-fix
<!-- ≤3: data loss, security, a broken contract, or a blocker for the next wave -->

## Should-fix
<!-- ≤6 -->

## Consider
<!-- ≤5 -->

## Keep as pattern
<!-- ≤5 bullets -->

## Previous findings
<!-- One line: resolved and still-open IDs, checked against the fix tasks -->

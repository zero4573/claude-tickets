---
title: {{title}}
created: {{date}}
updated: {{date}}
status: active
type: board
ticket: "[[TICKET-ID]]"
wave: 1
last-reviewed: {}
lead: none
lead-since: ""
po:
  session: ""
  last-refresh: ""
  exchanges-since: 0
  analyst-since: 0
  agents: []
tags: [board]
---
# Board

Tasks and wave reviews of [[TICKET-ID]] in [[provider-owner-repo]], kept by the lead; one row
each, updated in place.
Task files: `tasks/<ID>-NN-<slug>.md`, linked as `[[<ID>-NN-<slug>]]`. `last-reviewed` maps each
repo slug to the last reviewed commit. `lead:`, `lead-since:` and `po:` (its session, the
running agents, refresh counters) are the PO's, and so is marking `running` rows `orphaned`
when that session has ended; the lead writes the rest.

## Tasks
<!-- Status: planned · running · orphaned · merged <sha> · partial · blocked · dropped · pending — user. Note: ≤1 line. -->
| ID | Task | Repo | Wave | Deps | Status | Note |
|---|---|---|---|---|---|---|

## Reviews
<!-- M/S/C: must-fix / should-fix / consider counts. File: reviews/wave-N-<slug>.md -->
| Wave | Repo | Range | M/S/C | File | Fix tasks |
|---|---|---|---|---|---|

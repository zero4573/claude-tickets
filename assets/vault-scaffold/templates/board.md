---
title: {{title}}
created: {{date}}
updated: {{date}}
status: active
type: board
ticket: "[[TICKET-ID]]"
wave: 1
last-reviewed: {}
commit-style: ""
lead: none
lead-since: ""
po:
  session: ""
  last-refresh: ""
  exchanges-since: 0
  analyst-since: 0
  agents: []
  finished: []
  held: []
tags: [board]
---
# Board

Tasks and wave reviews of [[TICKET-ID]] in [[provider-owner-repo]], kept by the lead; one line
each, updated in place.
Task files: `tasks/<ID>-NN-<slug>.md`, linked as `[[<ID>-NN-<slug>]]`. `last-reviewed` maps each
repo slug to the last reviewed commit. `commit-style:` (`ticket`, `conventional` or `repo`, set at kickoff), `lead:`, `lead-since:` and `po:` (its session, the
running agents with their wave, finished notices not yet sent to the lead, refresh counters)
are the PO's, and so are the `## Specs` lines' `ready` mark and marking `running` lines `orphaned`
when that session has ended; the lead writes the rest.

## Tasks
<!-- One line per task: `- [<s>] NN · <status> · [[<ID>-NN-<slug>]] · <repo> · w<N> · deps <NN, …|-> · <note ≤1 line> ➕ <date>`
     `[ ]` planned · `[/]` running, orphaned, partial, blocked, pending — user · `[x]` merged <sha> ✅ <date> · `[-]` dropped ❌ <date> -->

## Specs
<!-- One line per capability: `- [ ] <slug>/<capability> · draft|ready|building|merged · needs <slug>/<capability>, …|- · <REQ IDs>`
     The PO marks `ready` (no clarification markers, its needs ready) and tells the lead at once; the lead marks `building` and `merged`. -->

## Reviews
<!-- Plain bullets (a review is not a task): `- w<N> · <repo> · <from>..<to> · M/S/C <m>/<s>/<c> · [[wave-N-<slug>]] · fix tasks <NN, …|lead <sha>|->`
     a pass with no review file: `- w<N> fix · <repo> · <from>..<to> · - · <what the lead checked> · -` -->

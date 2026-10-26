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
tags: [board]
---
# Board

Tasks and wave reviews of [[TICKET-ID]] in [[provider-owner-repo]], kept by the lead; one line
each, updated in place.
Task files: `tasks/<ID>-NN-<slug>.md`, linked as `[[<ID>-NN-<slug>]]`. `last-reviewed` maps each
repo slug to the last reviewed commit. `commit-style:` (`ticket`, `conventional` or `repo`, set at kickoff), `lead:`, `lead-since:` and `po:` (its session, the
running agents with their wave, finished notices not yet sent to the lead, refresh counters)
are the PO's, and so is marking `running` lines `orphaned`
when that session has ended; the lead writes the rest.

## Tasks
<!-- One line per task: `- [<s>] NN · <status> · [[<ID>-NN-<slug>]] · <repo> · w<N> · deps <NN, …|-> · <note ≤1 line> ➕ <date>`
     `[ ]` planned · `[/]` running, orphaned, partial, blocked, pending — user · `[x]` merged <sha> ✅ <date> · `[-]` dropped ❌ <date> -->

## Reviews
<!-- Plain bullets (a review is not a task): `- w<N> · <repo> · <from>..<to> · M/S/C <m>/<s>/<c> · [[wave-N-<slug>]] · fix tasks <NN, …|lead <sha>|->`
     a pass with no review file: `- w<N> fix · <repo> · <from>..<to> · - · <what the lead checked> · -` -->

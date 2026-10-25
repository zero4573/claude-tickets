---
name: grill
description: Grill the user on demand about new scope, a feature request or a plan (/tickets:grill [subject]), in rounds of AskUserQuestion with recommended answers, and record the decisions in the ticket's relay.md and ## Questions for the lead.
disable-model-invocation: true
argument-hint: "[subject: new scope, a feature, a plan]"
---

# /tickets:grill [subject]

1. Load the `tickets:grilling` skill with the Skill tool and follow it. The subject is the
   argument; without one, ask the user what to grill (new scope, a feature, the plan).
2. Depth: as many rounds as the subject needs; the user can say "skip" at any round.
3. In a ticket session (its `CLAUDE.md` names a ticket): record the results as grilling's
   "In a ticket" says (`relay.md` decision entries and the note's `## Questions`). If a lead
   runs in this session, SendMessage it one line: "grill decisions R-nn..R-mm". Otherwise the
   next lead plans them when it is spawned. New work the grill uncovered is a `request` entry.
4. Outside a ticket session: give the confirmed summary in the chat only.

Adapted from mattpocock/skills (MIT); see THIRD_PARTY_NOTICES.md.

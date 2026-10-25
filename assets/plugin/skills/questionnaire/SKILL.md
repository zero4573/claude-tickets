---
name: questionnaire
description: Turn a question the user can't answer alone, typically an open blocker owned by someone else, into a short questionnaire for that person to fill in, written to tickets/<ID>/<ID>-questionnaire-<slug>.md and linked from the ticket's #follow-up task. Offered by the PO in the kickoff's dependency step; use when the user needs facts or decisions from another person.
---

# questionnaire

**Ask the user only about the send**, which they can always answer; the document's questions
target the gap between what the recipient knows and what the user needs.

1. **Who and what** (one AskUserQuestion call, 2 questions): the recipient (role, expertise,
   relation to the user; offer the blocker's assignee first) and what's needed back (the
   decisions or facts this ticket waits on; offer what the `#follow-up` line says first).
   Facts you can look up (the blocker's status, its PR, the code) you look up yourself.
2. **Write** `tickets/<ID>/<ID>-questionnaire-<slug>.md` (slug: kebab-case topic, e.g.
   `<ID>-questionnaire-auth-token-format`) in the layout below. Every item from step 1 has a
   question.
3. **Link** it from the `#follow-up` line in the note's `## Follow-ups`: append
   `· questions: [[<ID>-questionnaire-<slug>]]` before the `➕` date. Tell the user the path;
   they send it. Answers they paste back go under each question's `>` stub, and the decisions
   into `## Questions` (and `relay.md` when a lead runs).

## Layout
```
---
title: <ID>-questionnaire-<slug>
created: <date>
updated: <date>
status: active
type: questionnaire
tags: [ticket, questionnaire]
ticket: "[[<ID>]]"
blocker: "[[<blocking ticket>]]"
to: <recipient>
---
# <Title>
**Purpose:** why it exists and the decision riding on it. **From:** <user> · **To:** <recipient>
· **Used for:** [[<ID>]], waiting on [[<blocking ticket>]].
## Context        (one paragraph for someone who wasn't in the user's head)
## How to answer  (deadline, rough effort; partial answers and "don't know" help)
## <Theme>        (one per theme once there are more than ~5 questions)
### <One question, never compound>
_Why this matters: <only where it could be misread or get a throwaway answer>_
>
## Anything else?
```
Questions go most important first (async: there may be only one pass).

Adapted from mattpocock/skills (MIT); see THIRD_PARTY_NOTICES.md.

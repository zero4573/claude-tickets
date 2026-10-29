---
name: clarify
description: Interview the user in rounds until you share one understanding of a plan, ticket, spec change or feature, mapping it as a design tree of decisions and asking the whole frontier each round with a recommended answer; /tickets:clarify questionnaire <who> instead turns questions the user can't answer alone into a document for that person. Used by the PO after a ticket's kickoff and by the spec flow; use whenever a plan, scope or feature needs stress-testing before work starts, or an open question is owned by someone else.
argument-hint: "[subject: new scope, a feature, a plan] | questionnaire <who>"
---

# /tickets:clarify [subject | questionnaire <who>]

Map the subject as a **design tree**: each decision branches into the decisions that depend on
it. The **frontier** is every open decision whose prerequisites are settled. Without a subject,
ask the user what to clarify. **Depth:** the caller scales it (a clear chore: one round; a vague
feature: several); there is no cap on rounds.

## Rounds
1. Ask the whole frontier at once, numbered `Q1`, `Q2`, … across the session, the decisions that
   change most first. A question that depends on one still open in this round waits.
2. Send it as **AskUserQuestion** calls of **≤4 questions** each (several calls for a wide
   frontier), so the user gets the needs-input notice. Per question: the recommended option
   first, labelled `(Recommended)`, 2–4 options, the tradeoffs in the option descriptions,
   and wording where "yes" accepts the recommendation. The user may answer "skip": stop, record
   what's settled, and keep each unanswered question as a `[NEEDS CLARIFICATION]` marker (in a
   spec: on its requirement) with the assumed answer.
3. Recompute the frontier from the answers and ask the next round.

**Security costs:** an auth, credential, token, grant or secret-handling choice states its
security tradeoff in the options (phishing, token lifetime, who can use it, blast radius), and
a choice that weakens security is asked as its own question, never folded into another.

**Facts are never the user's job.** A question that needs a fact (code, config, history,
another ticket) goes to a lookup instead: in a ticket session the PO spawns `tickets:analyst`
in the background; elsewhere look it up yourself. Don't block on it: only the questions that
depend on that fact wait. **Decisions are the user's**: never settle one silently.

**Done** when the frontier is empty: give a ≤8-line summary of the shared understanding and
ask the user to confirm it (AskUserQuestion: confirm / adjust). Don't act before they confirm.

## In a ticket
Once the user confirms, add relay entries only with `ct relay add <ID> --from user --to lead
--kind decision "<text>"` (it allocates the id and takes the lock; never edit `relay.md` by
hand). Each text is one line of at most 200 characters, so summarise a long decision to fit:
- on a ticket's first clarify, first the brief: write the confirmed summary of scope and
  decisions into `plan.md` under `## Scope`, then relay a pointer, `Brief: scope and decisions
  are in plan.md ## Scope`; then one per settled decision, `Qn: <decision, ≤200 characters>`;
- the note's `## Questions`: `- **yyyy-MM-dd** (clarify) Qn <question>` with
  `  - **Answer:** <answer>` under it; a skip is recorded as `skipped; assumed <…>`;
- the lead copies the `Qn:` decisions into `plan.md ## Decisions` (`ct relay state … copied`).
If a lead runs in this session, SendMessage it one line: "clarify decisions R-nn..R-mm";
otherwise the next lead plans them. New work uncovered is a `ct relay add … --kind request` entry. Outside a ticket
session give the confirmed summary in the chat only.

## Questionnaire mode
`/tickets:clarify questionnaire <who>`: a question the user can't answer alone, typically an
open blocker owned by someone else, becomes a short document for that person.
**Ask the user only about the send**; the document's questions target the gap between what the
recipient knows and what the user needs.
1. **Who and what** (one AskUserQuestion call, 2 questions): the recipient (role, expertise,
   relation to the user; `<who>` and the blocker's assignee first) and what's needed back (the
   decisions or facts this ticket waits on; the `#follow-up` line first). Look up facts yourself.
2. **Write** `tickets/<source>/<ID>/<ID>-questionnaire-<slug>.md` (slug: kebab-case topic, e.g.
   `<ID>-questionnaire-auth-token-format`) in the layout below; every item from step 1 has a question.
3. **Link** it from the `#follow-up` line in the note's `## Follow-ups`: append
   `· questions: [[<ID>-questionnaire-<slug>]]` before the `➕` date. Tell the user the path;
   they send it. Answers they paste back go under each question's `>` stub, and the decisions
   into `## Questions` (and `relay.md` when a lead runs).
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

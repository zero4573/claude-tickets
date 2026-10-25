---
name: grilling
description: Interview the user in rounds until you share one understanding of a plan, ticket or feature, mapping it as a design tree of decisions and asking the whole frontier each round with a recommended answer. Used by the PO after a ticket's kickoff and by /tickets:grill; use whenever a plan, scope or feature needs stress-testing before work starts.
---

# grilling

Map the subject as a **design tree**: each decision branches into the decisions that depend on
it. The **frontier** is every open decision whose prerequisites are settled.
**Depth:** the caller scales it (a clear chore: one round; a vague feature: several).

## Rounds
1. Ask the whole frontier at once, numbered `Q1`, `Q2`, … across the session. A question that
   depends on another one still open in this round waits for a later round.
2. Send it as **AskUserQuestion** calls of **≤4 questions** each (several calls for a wide
   frontier), so the user gets the needs-input notice. Per question: the recommended option
   first, labelled `(Recommended)`, 2–4 options, the tradeoffs in the option descriptions,
   and wording where "yes" accepts the recommendation. The user may answer "skip" at any
   round: stop, record what's settled, and list the rest as assumptions.
3. Recompute the frontier from the answers and ask the next round.

**Facts are never the user's job.** A question that needs a fact (code, config, history,
another ticket) goes to a lookup instead: in a ticket session the PO spawns `tickets:analyst`
in the background; elsewhere look it up yourself. Don't block on it: only the questions that
depend on that fact wait. **Decisions are the user's**: never settle one silently.

**Done** when the frontier is empty: give a ≤8-line summary of the shared understanding and
ask the user to confirm it (AskUserQuestion: confirm / adjust). Don't act before they confirm.

## In a ticket (the PO, or `/tickets:grill`)
- Each settled decision: one `relay.md` line,
  `R-nn · <date> · user→lead · decision · Qn: <decision> · open` (no `relay.md` yet: create it
  from the `relay` template; ids and caps as in the PO playbook, "relay.md"). The first decision of a ticket's first grill starts with
  `Brief: <what is being built and why, ≤120 chars>;`.
- The note's `## Questions`: `- **yyyy-MM-dd** (grill) Qn <question>` with
  `  - **Answer:** <answer>` under it; a skip is recorded as `skipped; assumed <…>`.
- The lead copies them into `plan.md ## Decisions` and sets them `planned`.

Adapted from mattpocock/skills (MIT); see THIRD_PARTY_NOTICES.md.

---
name: po-refresh
description: Shrink the ticket PO session's own context (/tickets:work-ticket) without touching the lead or its workers. Checkpoints everything the PO holds only in chat into board.md (lead:, po:) and relay.md, then prints the exact /compact command for the user to type. Use when the PO's context is heavy or the PO playbook suggests a refresh, as /tickets:po-refresh.
---

# po-refresh

A skill can't run built-in commands like `/compact` itself: checkpoint the state, then tell
the user what to type. Paths are in the vault, `tickets/<ID>/`.

## 1. Checkpoint (one or two calls)
Write what you hold only in this conversation:
- `board.md` frontmatter, with `sed -i` on those lines only: `lead: <agentId|none>`,
  `lead-since:`, and the `po:` block: `last-refresh: <today>`, `exchanges-since: 0`,
  `analyst-since: 0`, and `agents:` listing every agent still running
  (`"<agentId> · <agent> · task NN|review wN-<repo>|analyst · <since>"`, `[]` when none).
- `relay.md ## Open`: every pending user question and every request, feature or feedback the
  user gave that has no entry yet (`R-nn · … · open`); trim to the caps (`## Open` ≤30,
  `## Closed` ≤40).
- The ticket note's `status`: `blocked` while a user question is pending, else as the lead set it.

Then check that the PO playbook's resume (≤3 calls: board frontmatter and rows,
`tail -n 32 relay.md`, the note's status) would rebuild the picture: the lead id, the running
agents, the open questions and requests. Fix whatever is missing.

## 2. The command
Print it for the user, exactly, in a code block:
```
/compact Keep only: ticket <ID>, PO role (/tickets:work-ticket), lead id <agentId>, running agent ids <ids>; everything else is in board.md and relay.md
```
The conversation and its background subagents stay alive, and their completion notices still
arrive. Say: "the lead and workers keep running".

Guard: offer `/clear` then `/tickets:work-ticket <ID>` only when `lead:` is `none` and no board row is `running`.
Otherwise say `/clear` could orphan the lead and the running workers (whether background
subagents survive it is unverified), so use `/compact`.

## 3. After the refresh
Resume with the PO playbook ("0. Resume"): read `lead:` and `po:`, SendMessage the lead by id
"PO refreshed; send status", and carry on with the event loop. A lead that doesn't answer is
stale: set `lead: none` and spawn a fresh one (one lead only).

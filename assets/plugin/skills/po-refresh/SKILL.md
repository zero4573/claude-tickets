---
name: po-refresh
description: Shrink the ticket PO session's own context (/tickets:work-ticket) without touching the lead or its workers. Checkpoints everything the PO holds only in chat into board.md (lead:, po:) and relay.md, picks /clear when nothing is running, else /compact (or takes the one asked for), and prints the exact command for the user to type. Use when the PO's context is heavy, on your own when ct's context warning appears, or when the PO playbook suggests a refresh, as /tickets:po-refresh [clear|compact].
---

# po-refresh

`/tickets:po-refresh [clear|compact]`. A skill can't type built-in commands: checkpoint,
pick, print the command for the user. Run it **on your own** when ct's context warning
appears (at the next quiet point; past the last threshold at once): once checkpointed, the
session's auto-compact (`autoCompactWindow`) loses nothing. Paths: `tickets/<source>/<ID>/`.

## 1. Checkpoint (one or two calls)
Write what you hold only in this conversation:
- `board.md` frontmatter, with `sed -i` on those lines only: `lead: <agentId|none>`,
  `lead-since:`, and the `po:` block: `last-refresh: <today>`, `exchanges-since: 0`,
  `analyst-since: 0`, `agents:` listing every agent still running with its wave
  (`"<agentId> · <agent> · w<N>|- · task NN|review <repo>|analyst · <since>"`), and `finished:`
  listing every completion notice not yet sent to the lead (`"w<N> · task NN · done|partial"`,
  `"w<N> · review <slug>"`), each `[]` when none.
- `relay.md ## Open`: every pending user question and every request, feature or feedback the
  user gave that has no entry yet (`R-nn · … · open`); trim to the caps (`## Open` ≤30,
  `## Closed` ≤40) by the PO playbook's rule ("4. relay.md").
- The ticket note's `status`: `blocked` while a user question is pending, else the lead's.

Check the PO playbook's resume (≤3 calls: board, `tail -n 32 relay.md`, the note's status)
would rebuild the lead id, running agents, the buffered notices, open questions. Fix gaps.

## 2. Pick and print
**Safe to clear** only when `lead:` is `none` and no board row is `running` and `agents:` is `[]`
(what /clear does to running background agents is undocumented: never risk it).
- No argument: safe (nothing running) → the clear lines; else compact ("lead and workers keep running").
- `clear` and safe (none `running`): the clear lines. Not safe: warn (the lead, agent ids,
  waves still running) and ask with AskUserQuestion: **Wait** (when the guard holds after the
  last running agent's notice, say "Safe now:" and the clear lines) or **Compact now**.
- `compact`: the compact command (afterwards the session carries on by itself):
```
/compact Keep only: ticket <ID>, PO role (/tickets:work-ticket), lead id <agentId>, running agent ids <ids>; everything else is in board.md and relay.md
```
Clear lines (nothing running), one code block; say "type the first, then the second to resume
from board.md and relay.md":
```
/clear
/tickets:work-ticket <ID>
```

## 3. After the refresh
After a compact, ct's SessionStart hook points you at the PO playbook's "0. Resume";
`po: session:` still matches, so SendMessage the lead by id "PO refreshed; send status",
wait for its answer, carry on. Never replace it unless its completion notice arrived.

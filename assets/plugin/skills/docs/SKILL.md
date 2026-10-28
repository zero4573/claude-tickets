---
name: docs
description: Run a docs wave on the ticket's repos (/tickets:docs): one docs task per repo that writes or updates markdown under docs/ (architecture with a mermaid flowchart, one page per main flow with a sequenceDiagram) and links it from the README, then a review per repo. Use when the user runs /tickets:docs, asks to document what the ticket built, or when a task agent has skills [tickets:docs].
argument-hint: "[repo slug ...; default: every repo of the workspace]"
---

# /tickets:docs [repo ...]

Order: docs → tidy → `/tickets:save` (which pushes).

## Started by the PO

The user runs `/tickets:docs` in the PO session. Outside a ticket workspace (no
`board.md` next to `CLAUDE.md`), say so and stop. Otherwise append a `relay.md` entry
`R-nn · <date> · user→lead · request · docs wave: <repos or "all"> · open` and wake the
lead (SendMessage "new request R-nn"; no live lead: it is planned when the next one is
spawned). Reply with one line; the lead does the rest.

## The lead's part

1. One task per repo of the workspace (or the named ones): `skills: [tickets:docs]`, `owns`
   `docs/**` and `README.md`, checks of the repo. They run in parallel.
2. After their merge, one `tickets:reviewer` per repo on the docs diff; fix findings as
   usual (a small task per repo that needs it).
3. When the fixes are merged and the ticket worktrees are clean: `ct ws mark <ID> docs`.
   A later commit makes the mark stale, so run the wave last, before tidy.

## The task agent's part

Read the repo's README, `AGENTS.md`, the graphify graph and
`git diff <base>...HEAD --stat` (the base is in `workspace.json`). Then write or update
markdown under `docs/`:

- `docs/architecture.md`: the components and how they fit together, with a mermaid
  `flowchart`.
- one `docs/<flow>.md` per main data flow or workflow, with a mermaid `sequenceDiagram`.
- cover what the ticket changed and fill the gaps in the rest. Keep existing pages and
  update them in place rather than writing a second one on the same subject.
- the README gets a `## Documentation` section linking each page (update it if present).

Rules:
- Each page ≤300 lines. Quote flowchart labels (`A["label"]`) so punctuation can't break the
  render; in a `sequenceDiagram` write plain text (quotes show), without `;` or `#`.
- Describe what the code does now, checked against the code, not what the ticket planned.
- No vault-only IDs in pages, README or commits: task numbers, waves, `R-nn`, `B-nn`,
  review findings, or the key of a manual ticket. Only a remote ticket's key may appear.
- Commit in the ticket's commit style (the Git line of the workspace `CLAUDE.md`), a
  why-body, small coherent commits. Write only `docs/**` and `README.md`.

---
name: spec-init
description: Bootstrap a repo's living specs from its code (/tickets:spec-init <repo>), in a ct spec session. Gathers the vault notes, graph communities, README, docs and tests, gates a capability list with the user, files an "add specs for <repo>" ticket, has one tickets:spec-writer subagent draft each capability's spec.md (requirements inferred, with file:line and covering tests), and writes specs/README.md and a docs/architecture.md skeleton as drafts in the ticket folder. Shallow on purpose. Use when the user asks to add, bootstrap or write specs for a repo that has none.
argument-hint: "<repo slug>"
---

# /tickets:spec-init <repo>

Runs in a `ct spec` session: every repo is read-only, so the output is a ticket plus
drafts in the vault, never a commit. Spec format: `docs/spec-workflow.md`
("Layout and format"); the `tickets:spec` skill has it too. Without a repo, ask which.
Shallow on purpose: capabilities, interfaces, the main requirements. A spec deepens
when a ticket touches it.

## 1. Gather
- `ct vault search "<repo>" --project <slug> --budget 1500`, then `ct vault note get`
  by section: the project notes (features, sequences, data, decisions), `depends-on`,
  groups.
- The graph: `mcp__graphify__get_community` (and `god_nodes`) on the repo's nodes;
  communities are candidate capabilities.
- The repo's README, `docs/`, and its tests (names say what is promised).
Stop once you can list capabilities; don't read the whole repo.

## 2. Gate: the capability list
Show one line per capability (`<kebab-name>: what it does for its callers`), named by
what it does, never by a ticket. Ask the user to edit and approve it (AskUserQuestion,
or free text for edits). Draft nothing before the approval.

## 3. Location, asked once
`ct spec location <slug>`. With neither `specs/` in the repo nor a `specs:` field in its
project note, ask once (AskUserQuestion: repo, or vault only) and write the field.

## 4. File the ticket
Run `/tickets:new-ticket` for "add specs for <repo>" (title and source as it asks).
Its ID and source give the ticket folder `tickets/<source>/<ID>/`.

## 5. Draft, one subagent per capability
Output root `<out>`: for `specs: vault`, `tickets/<source>/<ID>/specs/<slug>/` (the
ticket-folder copy of the vault spec: `<out><capability>/spec.md`; make it first with
`ct spec start <ID> <slug>`, which promote needs); for `specs: repo`,
`tickets/<source>/<ID>/spec-init/` (the ticket's session copies it into the repo's
`specs/` after review). Spawn all `tickets:spec-writer` agents in one message, each with:
the repo slug and path, the capability name and its one line, the graph nodes and files
you found for it, the repo's `depends-on` (for Interfaces), and the exact output path
`<out><capability>/spec.md`. They write only that file and report in 5 lines.

## 6. Overview and architecture
Write, as the session (vault paths only):
- `<out>README.md`: the capabilities (one line each, linking their spec), interfaces
  provided and consumed (naming the other repos, from `depends-on`), cross-repo flows the
  repo owns.
- `<out>docs-architecture.md`: a skeleton for the repo's `docs/architecture.md` (this
  takes over the removed docs skill's architecture page): purpose, a mermaid `flowchart`
  of the main components and what they call, a section per capability pointing at its spec.
  Write it to `tickets/<source>/<ID>/docs-architecture.md`, outside `<out>`, so save never
  promotes it as a spec. The ticket's session copies it to `docs/architecture.md`.

## 7. Check and review
Run `ct spec check --dir <out>` on the drafts (it flags missing fields, more than 3
markers, duplicate IDs); fix what it flags. Read each draft once: every requirement is
`status: inferred` and has `file:line` evidence and covering tests (or says none), no
more than 3 `[NEEDS CLARIFICATION: ...]` per requirement, no invented behavior.

## 8. Hand over
Reply with the ticket ID, the drafts' paths, the capabilities, the open markers per
capability, and what the ticket's session does next: review the drafts, copy them to the
spec location, commit. Never edit the repo here. A requirement becomes `implemented`
only when a person confirms it.

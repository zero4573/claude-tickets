---
name: new-ticket
description: File a new ticket from a session, at a ticket source (e.g. Jira, through the source's MCP server when it has a create tool) or as a manual ticket (ct new), after the user confirms the source and the draft with AskUserQuestion. Prefers the current ticket's source, adds the new ticket to the current epic by default, and links it from the current ticket; ct sync brings a synced one into the vault at tickets/<source>/<ID>/. Use as /tickets:new-ticket [what], or when the user asks to create, open or file a ticket (a story, bug or task) at the source.
---

# /tickets:new-ticket [what]

Files one ticket the user asked for. Only a session the user talks to runs it (the PO, a `spec`
or repo session): it needs **AskUserQuestion**. Without it (headless, `-p`, a subagent),
create nothing: say what you would file and stop. A lead or task agent never files tickets;
it asks through `relay.md` (`lead` skill, "Never file a ticket on your own").

## 1. Context (no writes)
- **The session's ticket** `<ID>`: the workspace `CLAUDE.md` names it, its note and its folder
  (`tickets/<source>/<ID>/`). Read its frontmatter: `source`, `ticket-type`, `covers`,
  `covered-by`, `parent`, `projects`. A **lead ticket** is one with `ticket-type: epic` (its
  `covers` lists the tickets its workspace does). A `spec` or repo session has no ticket.
- **Sources:** `tickets/.sources.json` in the vault: each source's key, `enabled`, `sync`,
  `mcp` (the MCP server name) and `idPrefix`. The manual source files with `ct new`.
- **Create tool:** the source's MCP server (its `mcp` name; never another server) must
  expose a tool that creates a ticket. Jira through the Rovo MCP: `createJiraIssue` (with
  `getVisibleJiraProjects` and `getJiraProjectIssueTypesMetadata` for the project and the
  types; `atlassianUserInfo` or `lookupJiraAccountId` for the assignee). Rovo shows it only
  when the token has a write scope (`write:jira:agent-interface`). Other sources: a tool
  whose name and schema clearly create a ticket. Never use paid tools (`executeRead`, the
  unified `search`, Teamwork Graph; the vault's `AGENTS.md`, "Cost guard").
- **Duplicates:** `ct vault search "<topic>" --budget 1500` and, for Jira, a JQL
  `summary ~ "<words>"` search in the project. A match: show it and ask whether to file at all.

## 2. Draft
- **Source:** the session ticket's `source` when it can create (a synced source with a
  create tool), else the one the user named; outside a ticket, ask. A manual session ticket
  drafts a manual ticket.
- **Project / where:** for Jira, the session ticket's project (`PROJ` of `PROJ-12`).
- **Title** (one line), **type** (the source's types, e.g. Story, Task, Bug; for `ct new`:
  `dev`, `bug`, `investigation`, `chore` or `epic`), **description** (what and why, the
  evidence, acceptance criteria as a list; no vault-only IDs such as task numbers, waves,
  `R-`/`B-` entries or findings), **assignee** (the user by default: `ct sync` only pulls
  the user's open tickets, so an unassigned or someone else's ticket gets no vault note).
- **Parent:** when the session ticket is a lead (`ticket-type: epic`), the new ticket goes
  under it by default: the source's parent / epic link (Jira: `parent: {key: <ID>}`) for a
  synced epic. When the session ticket is itself covered (`covered-by`), offer that lead.

- **Jira task lists:** the Rovo `createJiraIssue` tool takes `contentFormat` `markdown` (the
  default) or `html`, and markdown `- [ ] item` renders as a plain bullet prefixed "[]". When
  the description has a task list (e.g. the acceptance criteria as checkboxes), file it with
  `contentFormat: "html"` and write the whole description as HTML: `<p>`, `<h3>`, `<ul>` /
  `<ol>` / `<li>`, `<code>`, `<a href>`, and each task list as
  `<ul data-type="task-list"><li data-type="task-item"><input type="checkbox"> Item</li></ul>`
  (`<input type="checkbox" checked>` for a done item: `- [x]` in the draft). Escape `&`, `<` and `>` in
  the text as `&amp;`, `&lt;`, `&gt;`. Without a task list keep markdown.
  The same rule applies to any `editJiraIssue` description edit; never add outward edits.

## 3. Confirm (always, before anything is created)
One AskUserQuestion call: show the source, project, type, title, assignee, parent and the
description (in the question text), with options such as *File it at `<source>`* (recommended),
*File it as a manual ticket*, *Change it* (then redraft and ask again), *Don't file it*. Under a
lead add the choice *not under `<lead>`*. Nothing is filed without a yes, and only what was shown.

## 4. File it
**At the source:** call the create tool with the confirmed fields. For Jira with a task list, pass
`contentFormat: "html"` and the HTML description (the Jira task lists rule above); the Confirm step shows
the readable markdown draft, and what is filed must say exactly the same. Keep the key it returns
(`PROJ-13`) and the URL. On an error, a missing create tool, or a permission refusal (401/403,
"not allowed", read-only token): don't retry another way; tell the user why and ask
(AskUserQuestion) whether to file it as a manual ticket instead, or not at all.

**Manual** (`ct new`, which files into this vault at `tickets/manual/<MAN-n>/`):
`ct new --type <dev|bug|investigation|chore|epic> [--priority <p>] "<title>"`. It prints the new
note's path. Write the new note's initial content yourself, in that note only: `## Description`,
`## Acceptance criteria`, `## Context / links` (`[[<ID>]]`), `projects:`, the tag
`from-ticket` (`from-spec` in a `spec` session), and under a lead `covered-by: "[[<lead>]]"` and
`parent: "[[<lead>]]"`. From then on it is the user's.
Tell the user it can be filed at a source later with `ct ticket convert <MAN-n> --to <source>` (run
on the host, with its session closed).

## 5. Link it into the vault
Who writes the new ticket's note depends on where it was filed:
- **A synced source:** under a lead `ct ticket stub` writes it (step 2), else `ct sync` does, at
  `tickets/<source>/<NEW-ID>/<NEW-ID>.md`.
- **Manual** (`ct new`): step 4 wrote it, and it is the user's from then on.
1. In the session ticket's note, add a line to `## Follow-ups` (the work section you may
   write; `ct vault note append <ID> "## Follow-ups"`):
   - at a source: `- [ ] Filed [[<NEW-ID>]] at <source> from this ticket: <title> (<url>); ct sync brings its note in #follow-up ➕ <today>`
   - manual: `- [ ] Filed [[<MAN-n>]] from this ticket: <title>; review it #follow-up ➕ <today>`
   The wikilink resolves once the note exists. Outside a ticket, the `spec` session's draft or
   the answer names it instead.
2. **Under a lead, filed at a synced source** (never a manual child, which `ct ticket stub`
   refuses): right after the create, run
   `ct ticket stub <NEW-ID> --source <source> --summary "<title>" --covered-by <lead>`. It writes
   the child's note in the synced layout and the child's `covered-by`, and, for a manual lead,
   adds the child to the lead's `covers`; don't add a follow-up "add to `covers`" line for it,
   and don't write that note by hand. A synced epic's `covers` comes from the source: `ct sync`
   keeps it. The PO tells the lead in its next message. A lead running in this workspace hears
   of it in your next message to it (new covered work: `New since the last lead: filed <NEW-ID>`),
   never by editing its files.
   **Under a manual lead, a manual child** (`ct new`): you write the note (step 4) with its
   `covered-by`, and say "add `[[<MAN-n>]]` to `covers` of [[<lead>]]" in the follow-up line:
   a manual lead's `covers` is the user's field there.
3. **Sync:** `ct sync --source <source>` on the host (where its MCP config is) brings a synced
   note in; tell the user to run it.

## 5b. Epic flow (a feature with specs)
Planned work from `/tickets:spec plan` files an epic and children: read `epic.md` in this skill's
folder before step 3. Each child description has a `## Spec change` section; a requirement with a
`[NEEDS CLARIFICATION]` marker blocks filing that child.

## 6. Report
One line per filed ticket: the ID, where (source or manual), its URL or note path, the parent,
and what is left for the user (`ct sync`, a manual lead's `covers`).

# Epic flow

Used when the user wants a feature filed as an epic with one child per capability or slice
(from `/tickets:spec plan`, or asked directly). Same rules as the main skill: AskUserQuestion,
no filing without a yes, ticket writes through the source's MCP tools.

1. **Project:** ask which project (for Jira, the project key) the work goes in.
2. **Epic:** search for an existing one (`summary ~ "<words>"`, `issuetype = Epic`, and
   `ct vault search`). Show matches and ask: use it, or create one. Create the epic first
   (Jira: issue type Epic; manual: `ct new --type epic`) and keep its key.
3. **Children, in order:** one per capability or slice, each with `parent` set to the epic. Confirm each draft
   (or the whole set in one AskUserQuestion that lists every title and description) before creating.
4. **Description:** the usual sections plus a visible `## Spec change` heading holding the
   capability, requirements (`### REQ-<PREFIX>-<n> · <title>`, `status: proposed`, SHALL text) with
   their `#### Scenario:` blocks, and the interface changes. Self-contained: a teammate reads only
   the ticket. For Jira use the task-list rule of the main skill when acceptance criteria are checkboxes.
5. **Marker gate:** a requirement with a `[NEEDS CLARIFICATION]` marker blocks filing that
   child. Go back to clarify for it (`/tickets:clarify`), or leave the child unfiled and say so.
   The other children are not blocked unless they consume its interface.
6. **`spec-draft.md`:** right after each child is filed, update
   `tickets/<source>/<EPIC>/spec-draft.md` with what doesn't fit a description (why, rejected
   options, open questions, cross-repo notes) and the children filed so far. `ct sync` leaves it
   alone. Write it only in the epic's own ticket folder.
7. **Link:** as in step 5 of the main skill, for the epic and each child; the epic's `covers`
   follows the source, or is the user's for a manual epic.

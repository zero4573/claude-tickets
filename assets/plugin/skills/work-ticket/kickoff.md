# work-ticket: kickoff

Read by the PO (`/tickets:work-ticket <ID>`) when the ticket note's `## Workspace` is
empty, and again for a repo that turns out to be needed later (the lead asks for it through
`relay.md`). It ends with the worktrees created, `## Workspace`, `relay.md` and `board.md`
written and the grill (4) done; then spawn the lead ("1. Spawn the lead" in `SKILL.md`). A
repo added later skips the grill.

## 1. Load context

1. Read `CLAUDE.md`, `workspace.json`, the vault's `AGENTS.md` (by section:
   `rg -n '^## '`), and the ticket note `tickets/<ID>/<ID>.md` (its content is the source
   block, or the description sections for a manual ticket). Read linked tickets that exist in
   the vault, especially its `parent`, `blocked-by`, `blocks` and, for a lead, every ticket in
   `covers` and `children`.
2. If the ticket note's `status` is `new`, set it to `triage`, and add the line
   `- [/] Kickoff: repos, branches, versions #ticket/kickoff ➕ <today>` to `## Tasks`.
3. A manual ticket whose `## Description` is empty or too thin to act on: ask the user to fill
   it in (or to answer your questions) before going on. Don't invent requirements.
4. **Classify.** Use `ticket-type` from the note (`dev`, `bug`, `investigation`, `chore`,
   `epic`). If the ticket's content clearly contradicts it (e.g. a "Task" that is really a bug
   report), say so and use the better fit. Update `ticket-type`, which the kickoff owns. The
   type shapes the lead's plan ("Ticket types" in the `tickets:lead` skill), not the agents.

## 2. Lead tickets (`ticket-type: epic`)

An epic, or any ticket with `ticket-type: epic`, is a **lead**. Its workspace does the work of
every ticket in its `covers` list. Those are the children assigned to the user, and
`ct start` sends them here. The lead agent runs it like one bigger ticket:
- **One workspace, one branch per repo:** `feature/<LEAD-ID>[-<description>]` for all the
  covered tickets together.
- **One plan** over all of them: `plan.md` has scope and acceptance criteria per covered
  ticket, and each task file names the ticket it serves with `for: "[[<ID>]]"`.
- **Covered tickets' notes:** keep their `status` in step with their part of the work
  (`in-progress`, then `review`), and give each a one-line `## Workspace` entry:
  `Worked in [[<LEAD-ID>]]` plus the branch.
- **Children that aren't yours** (in `children` but not `covers`, marked *(not yours)* in the
  source block) are other people's work. Treat them like blockers: see step 0 below.

## 3. Kickoff gate (always, before any worktree exists)

0. **Dependencies.** Before choosing repos, check what this ticket waits on: its `blocked-by`
   tickets and, for a lead, its children that aren't yours. For each one, take its status
   from its note if it has one (`status`, `source-status-category`), otherwise look it up in
   the source (for Jira, `getJiraIssue`, which is free). Then:
   - **Done:** nothing to do.
   - **Not done:** add a follow-up in the ticket note's `## Follow-ups`:
     `- [ ] Follow up with <assignee> on [[<ID>]] (<status>): <what this ticket needs from
     it> #follow-up ➕ <today>`. The user tracks these in the Tasks views. Mark one `[x]` with
     `✅` once it's no longer blocking.
   - **Owned by someone else and needs answers from them:** offer the user a questionnaire
     for that person (the `tickets:questionnaire` skill). It writes
     `tickets/<ID>/<ID>-questionnaire-<slug>.md` and links it from the `#follow-up` line.
   - **If an open blocker stops real progress**, ask the user with AskUserQuestion:
     - **wait:** keep the ticket's `status` (`blocked` means waiting on the user;
       `blocked: true` already says it waits on another ticket), note the wait under
       `## Review`, and stop after the follow-ups;
     - **stack on its branch:** when the blocker has a workspace or an open PR, use its branch
       as `--base` in step 4, so this work builds on it (the PR then merges after the
       blocker's);
     - **go ahead independently**, e.g. against a stub or an agreed interface.
     Record the decision under `## Questions`.
1. Work out which **local** repos are involved: query the graph with the ticket's key terms
   (endpoints, entities, error messages, repo names) and use `shortest_path` between repos.
   Add the neighbours `ct vault groups <vault> <slug>` shows for each candidate (its
   `depends-on`, used-by and fellow group members) as candidates too. List the repos and
   their slugs with `ct ws repos`.
2. Look for related repos on **Bitbucket** that aren't cloned yet. The graph only knows about
   local repos. Use the `atlassian` MCP server (`discover`, then `executeRead`; Bitbucket
   calls need no `cloudId`):
   - `listBitbucketWorkspaces` for the workspaces.
   - For each workspace, `listBitbucketWorkspacePullRequests` with `titleContains: <ID>`
     (any state). The repos of PRs that already mention the ticket are strong candidates, and
     an open one's source branch can be reused (`--branch`). The listing has no branches:
     take the repo from `links.html.href` and the branches from `getBitbucketRepoPullRequest`.
   - `listBitbucketRepositories` with a BBQL `q` per key term, e.g. `name ~ "orders"`. Use
     the repo names, components and labels the ticket mentions, and the names of repos the
     graph shows talking to the candidates (a client, consumer or shared library).

   A remote repo's slug is `bitbucket-<workspace>-<repo>` and its clone path is
   `~/Projects/bitbucket/<workspace>/<repo>`. Anything not in `ct ws repos` is **not
   cloned**. Keep the list to repos with a concrete reason to be involved.
3. Ask the user with **AskUserQuestion**. The ticket often says nothing about branches or
   versions, so always ask:
   - **Repos:** multiSelect of your candidates by slug, each with a one-line reason. Mark the
     ones that are `(not cloned)`, and note any existing branch or PR for this ticket. The
     user can add others.
   - If any chosen repo isn't cloned, the sandbox can't clone it (there are no git
     credentials). Give the user the exact host command, one per repo:
     `ct ws clone bitbucket/<workspace>/<repo>`. Then ask whether it's done, or whether to go
     on without that repo. Once `~/Projects/bitbucket/<workspace>/<repo>/.git` exists,
     `ct ws add` works for it in this session. It makes a shared clone because the new repo's
     `.git` isn't writable here, and the graph picks it up within about 15 seconds.
   - For each chosen repo, the **base branch** to branch from. Offer the likely ones you
     found with `git -C ~/Projects/<provider>/<owner>/<repo> branch -r`, e.g. `develop`,
     `main`, `release/x.y`.
   - For each chosen repo, the **target version** the change ships in. Offer what the repo's
     version file, latest tag (`git describe --tags --abbrev=0 origin/<base>`) and the
     ticket's `source-fix-versions` suggest.
   - An optional short **branch description** (kebab-case), giving
     `feature/<ID>-<description>`.

   Batch these (up to 4 questions per call; several calls are fine).
4. For each repo, run
   `ct ws add <ID> <slug> --base <base> --version <version> [--desc <description>]`.
   Add `--branch <name>` to reuse an existing branch for this ticket, e.g. an open PR's
   source. It records the repo in `workspace.json`.
5. Write the answers into the ticket note:
   - a `## Workspace` table with the `workspace.json` fields per repo:
     | Repo (slug) | Base | Target version | Branch | Path |
     (`slug`, `base`, `targetVersion`, `branch`, `path`)
   - frontmatter `projects: [<slug>, ...]`
   - the kickoff line in `## Tasks` marked `[x]` with `✅ <today>`.
6. If a repo turns out to be needed later, local or not, run this same gate for it before
   adding it (and before any task names it as `repo`).
7. On a new ticket (no `relay.md` or `board.md` yet), whether or not a grill follows: create
   both in `<vault>/tickets/<ID>/` from `<vault>/templates/relay.md` and `board.md`, filling
   `{{title}}`, `{{date}}`, `TICKET-ID` and the board's `provider-owner-repo` (the first slug).

## 4. Grill (always on a new ticket, before the lead)

Load the `tickets:grilling` skill and grill the user on the ticket's scope, scaled to it: one
round for a clear `chore` or a well-specified bug, several for a vague `dev` ticket or an
epic. Start from what the ticket, the linked tickets and the kickoff left open (behaviour,
edge cases, acceptance criteria, compatibility, what's out of scope); never re-ask the repos,
branches or versions. Facts go to a background `tickets:analyst`. The user can say "skip" at
any round. Record the results as grilling's "In a ticket" says (`relay.md` decision entries,
the first carrying the `Brief:`, and `## Questions`); the lead copies them into `plan.md`.

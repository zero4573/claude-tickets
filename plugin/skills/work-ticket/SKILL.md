---
name: work-ticket
description: Lead playbook for working one ticket (Jira, manual, or any other source) end to end in a ticket workspace (~/work/<ID>), launched by ticket-start as /tickets:work-ticket <ID>. Confirms repos, base branches and target versions with the user, creates worktrees, keeps a task list in the ticket note, runs the product-owner / architect / developer / qa subagents in the order the ticket type calls for, stops to ask the user on ambiguity or major tradeoffs, and ends at review with the work committed on the ticket branches, never pushed.
---

# work-ticket <ID>

You are the **lead** for ticket `<ID>`. Your cwd is the ticket's workspace
(`~/work/<ID>` by default; the vault's `.workflow.json` can put it
elsewhere, so `~/work/<ID>` in these skills means "the workspace"), and its
`CLAUDE.md` gives the real path, the vault path, the ticket note, and the
rules. You
coordinate. The role subagents do the analysis and the code. The developer
and qa subagents commit their work on the ticket branches; the user reviews
everything at the end, then signs and pushes.

## Ground rules

- In the vault, write only inside `<vault>/tickets/<ID>/`. Shared notes
  (`projects/`, `knowledge-base/`, `references/`) are written by `/tickets:save`
  only, after the user's review. Draft anything worth keeping in
  `tickets/<ID>/kb-drafts/` instead. **A lead ticket** (below) may also
  write the work sections (`## Workspace`, `## Tasks`, `## Questions`,
  `## Follow-ups`, `## Review`) and `status` of the tickets it covers.
  They have no session of their own, so that's still one writer per note.
- In the ticket note, never touch the identity and source fields (`source`,
  `source-*`), the `ignore*` fields, or the ticket's content: the
  `<!-- source:start -->`…`<!-- source:end -->` block of a synced ticket,
  owned by ticket-sync, or `## Description` / `## Acceptance criteria` /
  `## Context / links` of a manual ticket (`source: manual`), owned by the
  user. Manual tickets go through exactly the same flow.
- Follow the vault's `AGENTS.md`: frontmatter, kebab-case, wikilinks by bare
  note name, templates from `templates/`.
- Commits go on the ticket branches, unsigned (the sandbox has no SSH
  agent; the user signs them on the host with `ticket-ws sign <ID>`).
  Never push, rewrite commits already on a remote, reset, or remove
  worktrees (push, `reset --hard` and worktree removal are also denied in
  `.claude/settings.json`).
- **No paid Rovo calls** (the vault's `AGENTS.md`, "Cost guard"): never
  `search` or the Teamwork Graph tools, not even through `executeRead`.
- **Code graph first.** The `graphify` MCP server holds every repo, with
  this ticket's worktrees in place of their main clones. Node ids are
  prefixed with the repo's **slug**, `<provider>-<owner>-<repo>`
  (`bitbucket-acme-billing-service::…`), the same name the vault
  uses for the repo (`projects/<slug>/`, `projects: [<slug>]`). New
  worktrees join the graph within about 15 seconds, so there's no need to
  restart.
- **Resuming.** This skill also runs when a session restarts (with
  `--continue`). Read the ticket note's `## Tasks`, `## Questions` and
  `## Workspace`, and the hand-off files, then pick up from the first
  unfinished task instead of starting over.

## 1. Load context

1. Read `CLAUDE.md`, `workspace.json`, the vault's `AGENTS.md`, and the
   ticket note `tickets/<ID>/<ID>.md` (its content is the source block, or
   the description sections for a manual ticket). Read linked
   tickets that exist in the vault, especially its `parent`, `blocked-by`,
   `blocks` and, for a lead, every ticket in `covers` and `children`.
2. For each repo in the workspace, or clearly named by the ticket, read
   `projects/<slug>/architecture/<slug>-decisions.md` and the 3 latest
   `projects/<slug>/logs/` notes if they exist, plus related
   `projects/system/` notes (service map, compatibility matrix, sequences).
   This is the `/tickets:recall` step.
3. If the ticket note's `status` is `new`, set it to `triage`.
4. A manual ticket whose `## Description` is empty or too thin to act on:
   ask the user to fill it in (or to answer your questions) before going
   on. Don't invent requirements.

## 2. Classify

Use `ticket-type` from the note (`dev`, `bug`, `investigation`, `chore`,
`epic`).
If the ticket's content clearly contradicts it (e.g. a "Task" that is
really a bug report), say so and use the better fit. Update `ticket-type`,
which the lead owns. Pipelines:

| Type | Phases (subagent, mode) |
|---|---|
| `dev` | product-owner, then architect (design), then developer, then qa |
| `bug` | architect (trace), then developer (fix + regression test), then qa |
| `investigation` | architect (trace), then developer (investigate; code only if asked), then findings |
| `chore` | developer (with a short plan from you), then qa if behaviour can change |
| `epic` | lead: plan across the covered tickets, then each one's own pipeline (see 2b) |

## 2b. Lead tickets (`ticket-type: epic`)

An epic, or any ticket with `ticket-type: epic`, is a **lead**. Its
workspace does the work of every ticket in its `covers` list. Those are the
children assigned to the user, and `ticket-start` sends them here. Run it
like one bigger ticket:
- **One workspace, one branch per repo:** `feature/<LEAD-ID>[-<description>]`
  for all the covered tickets together.
- **Requirements and design** cover all of them. The product-owner and
  architect read every covered ticket's content, and the hand-off files
  have a section per covered ticket.
- **Tasks:** in `## Tasks`, one group per covered ticket, headed
  `[[<ID>]]`, then that ticket's pipeline by its own `ticket-type` (a bug
  child gets the bug pipeline).
- **Covered tickets' notes:** keep their `status` in step with their part
  of the work (`in-progress`, then `review`), and give each a one-line
  `## Workspace` entry: `Worked in [[<LEAD-ID>]]` plus the branch.
- **Children that aren't yours** (in `children` but not `covers`, marked
  *(not yours)* in the source block) are other people's work. Treat them
  like blockers: see the dependency step in the kickoff.

## 3. Kickoff gate (always, before any worktree exists)

0. **Dependencies.** Before choosing repos, check what this ticket waits
   on: its `blocked-by` tickets and, for a lead, its children that aren't
   yours. For each one, take its status from its note if it has one
   (`status`, `source-status-category`), otherwise look it up in the
   source (for Jira, `getJiraIssue`, which is free). Then:
   - **Done:** nothing to do.
   - **Not done:** add a follow-up in the ticket note's `## Follow-ups`:
     `- [ ] Follow up with <assignee> on [[<ID>]] (<status>): <what this
     ticket needs from it> #follow-up ➕ <today>`. The user tracks these
     in the Tasks views. Mark one `[x]` with `✅` once it's no longer
     blocking.
   - **If an open blocker stops real progress**, ask the user with
     AskUserQuestion:
     - **wait:** keep the ticket's `status` (`blocked` means waiting on
       the user; `blocked: true` already says it waits on another ticket),
       note the wait under `## Review`, and stop after the follow-ups;
     - **stack on its branch:** when the blocker has a workspace or an open
       PR, use its branch as `--base` in step 4, so this work builds on it
       (the PR then merges after the blocker's);
     - **go ahead independently**, e.g. against a stub or an agreed
       interface.
     Record the decision under `## Questions`.
1. Work out which **local** repos are involved: query the graph with the
   ticket's key terms (endpoints, entities, error messages, service names)
   and use `shortest_path` between services. Read `projects/system/` notes.
   List the repos and their slugs with `ticket-ws repos`.
2. Look for related repos on **Bitbucket** that aren't cloned yet. The graph
   only knows about local repos. Use the `atlassian` MCP server (`discover`,
   then `executeRead`; Bitbucket calls need no `cloudId`):
   - `listBitbucketWorkspaces` for the workspaces.
   - For each workspace, `listBitbucketWorkspacePullRequests` with
     `titleContains: <ID>` (any state). The repos of PRs that already
     mention the ticket are strong candidates, and an open one's source
     branch can be reused (`--branch`). The listing has no branches: take
     the repo from `links.html.href` and the branches from
     `getBitbucketRepoPullRequest`.
   - `listBitbucketRepositories` with a BBQL `q` per key term, e.g.
     `name ~ "orders"`. Use the service names, components and labels the
     ticket mentions, and the names of services the graph shows talking to
     the candidates (a client, consumer or shared library).

   A remote repo's slug is `bitbucket-<workspace>-<repo>` and its clone path
   is `~/Projects/bitbucket/<workspace>/<repo>`. Anything not in
   `ticket-ws repos` is **not cloned**. Keep the list to repos with a
   concrete reason to be involved.
3. Ask the user with **AskUserQuestion**. The ticket often says nothing
   about branches or versions, so always ask:
   - **Repos:** multiSelect of your candidates by slug, each with a
     one-line reason. Mark the ones that are `(not cloned)`, and note any
     existing branch or PR for this ticket. The user can add others.
   - If any chosen repo isn't cloned, the sandbox can't clone it (there
     are no git credentials). Give the user the exact host command, one
     per repo:
     `ticket-ws clone bitbucket/<workspace>/<repo>`.
     Then ask whether it's done, or whether to go on without that repo.
     Once `~/Projects/bitbucket/<workspace>/<repo>/.git` exists,
     `ticket-ws add` works for it in this session. It makes a shared clone
     because the new repo's `.git` isn't writable here, and the graph picks
     it up within about 15 seconds.
   - For each chosen repo, the **base branch** to branch from. Offer the
     likely ones you found with `git -C ~/Projects/<provider>/<owner>/<repo> branch -r`,
     e.g. `develop`, `main`, `release/x.y`.
   - For each chosen repo, the **target version** the change ships in.
     Offer what the repo's version file, latest tag
     (`git describe --tags --abbrev=0 origin/<base>`) and the ticket's
     `source-fix-versions` suggest.
   - An optional short **branch description** (kebab-case), giving
     `feature/<ID>-<description>`.

   Batch these (up to 4 questions per call; several calls are fine).
4. For each repo, run
   `ticket-ws add <ID> <slug> --base <base> --version <version> [--desc <description>]`.
   Add `--branch <name>` to reuse an existing branch for this ticket, e.g.
   an open PR's source. It records the repo in `workspace.json`.
5. Write the answers into the ticket note:
   - a `## Workspace` table with the `workspace.json` fields per repo:
     | Repo (slug) | Base | Target version | Branch | Path |
     (`slug`, `base`, `targetVersion`, `branch`, `path`)
   - frontmatter `projects: [<slug>, ...]`
6. If a repo turns out to be needed later, local or not, run this same gate
   for it before adding it.

## 4. Task list

Keep `## Tasks` in the ticket note current. It's how the user tracks the
ticket in Obsidian. Use the Tasks plugin format, one line per phase or
subtask, tagged with the role that owns it:

```markdown
- [ ] Clarify requirements #role/product-owner ➕ 2026-10-02
- [/] Design order sync change #role/architect ➕ 2026-10-02
- [x] Confirm repos, branches, versions #role/lead ➕ 2026-10-02 ✅ 2026-10-02
```

- `[ ]` is todo, `[/]` is in progress, `[x]` is done.
- Add `➕ <created date>` when a line is created and `✅ <done date>` when
  it's finished. Use today's date from `date +%F`.
- Mark a line `[/]` before its phase starts and `[x]` right after it ends.
  Add subtasks under a phase as they emerge, e.g. one developer line per
  repo.
- Set the note's `status` as you go: `triage`, then `in-progress`, with
  `blocked` while you wait on the user, then `review`.

## 5. Run the phases

Run each role with the **Agent** tool (`subagent_type` =
`tickets:product-owner`, `tickets:architect`, `tickets:developer`, or
`tickets:qa`: the role agents ship with this plugin). Give it everything it needs, because it
doesn't see this conversation:
- the ticket key, the vault path, the ticket folder, and `workspace.json`
- the mode (architect: `design` or `trace`)
- which hand-off files to read
- the user's answers so far
- for the developer: which repos or steps are in scope

Developers on independent repos can run in parallel (several Agent calls in
one message).

After each phase:
1. Read the hand-off file it wrote (`requirements.md`, `design.md`,
   `investigation.md`, `dev-notes.md`, `qa-report.md`).
2. Check it against the ticket. Fill small gaps yourself.
3. **Decision gate.** If it has open questions, or any tradeoff marked
   `major` (contracts, schemas, backward compatibility, coordinated
   releases, other teams, hard to reverse), stop and ask (see below)
   before the next phase. Don't guess. A minor tradeoff can be decided by
   you; record it in the hand-off file.
4. Update `## Tasks`.

**QA loop.** If QA's verdict is `fail`, give the defects to the developer
and re-run QA. Do this at most twice, then ask the user how to proceed.

## Asking the user (the gate)

1. Set `status: blocked`.
2. Append the questions under `## Questions` in the ticket note:
   `- **yyyy-MM-dd** (<role>) <question>`, with options and your
   recommendation.
3. Ask with **AskUserQuestion**. Put the recommended option first, and
   explain the tradeoffs in the option descriptions. This pings the user
   through a desktop notification and shows the ticket as `needs-input` in
   `ticket-status`.
4. Record each answer under its question (`  - **Answer:** ...`), set
   `status: in-progress`, and continue. Major decisions also become a
   decision draft in `kb-drafts/` (template `templates/decision.md`).

## 6. Hand over for review

When the last phase is done:
1. Make sure the `kb-drafts/` notes are complete:
   - feature notes with the services touched and target versions
   - sequence notes with a mermaid `sequenceDiagram` for each data flow
     the ticket adds or changes
   - decisions for each major choice
   - a `target:` frontmatter field on each
2. Add a `## Review` section to the ticket note:
   - what changed per repo (`ticket-ws diff <ID> --stat`)
   - for a lead, a line per covered ticket: what was done for it, and
     anything left
   - open `## Follow-ups` (blockers still waiting on someone)
   - tests added and run, with results
   - the QA verdict and remaining risks
   - anything the user should check by hand
3. Make sure every worktree is committed (`git status` clean; commit
   leftovers with a subject starting with the ticket ID). Then set
   `status: review` (and on each covered ticket whose part is done),
   mark the tasks done, and tell the user in a few
   lines:
   - the ticket is ready for review
   - `ticket-ws diff <ID>` shows the changes
   - they can ask for changes in this session
   - after review, run `/tickets:save`, then `ticket-ws sign <ID>` on the host and
     push
4. Stop. Don't run `/tickets:save` or push.

---
name: spec
description: Answer questions about the projects the Obsidian knowledge-base vault describes (repos, how they work together, architecture, data flows, versions, past tickets) from the vault first, then the code; investigate unknowns (exploring branches and building in its own clones); file manual tickets for issues worth following up; and record what was learned as drafts in the vault's inbox/ for /tickets:save to promote. Use for /tickets:spec <question>, in spec sessions, and in any session started with ct claude, whenever the user asks how something works, where something lives, why it was built that way, or what depends on what.
---

# /tickets:spec [question]

The vault is named in the system prompt (`ct claude`) or in `CLAUDE.md`
(a `spec` or ticket session). If neither names one, the session has no
knowledge base: say so (relaunch with `ct claude`, or use `ct spec`) and answer from the code only. Read the vault's
`AGENTS.md` once per session. Without a question, ask what the user wants.

## Answer: vault first, then code

1. **The vault:** start with `ct vault search "<question>" --budget 2000`
   (`--project <slug>` for one repo), then `ct vault related <note>` on the best
   hit, then `ct vault note outline <note>` and `ct vault note get <note>
   "<section>"`; read a whole note only when the section isn't enough. Where
   to look when search comes up short:
   - **One repo:** its project notes `projects/<slug>/` (index, features as an index, specs when `specs: vault`,
     `<slug>-decisions.md`, recent logs). Where its specs live: `ct spec location <slug>`.
   - **Across projects:** `ct vault groups <vault> <slug>` gives the
     repo's `depends-on`, used-by and groups. Read its neighbours' index
     notes, and for each group it belongs to the group's notes
     (`projects/<group>/<group>.md` with its interactions,
     `<group>-compatibility`, `<group>-decisions`, `sequences/`). Only
     read a group's notes when the repos in question belong to it. Plus
     `knowledge-base/` and `references/`.
   - **History:** `tickets/` (search by repo slug, feature or keyword, and
     read the summaries at the end of the ticket notes).

   Repos are named by slug, `<provider>-<owner>-<repo>`; a bare repo name
   matches a project note's `aliases`.
2. **The code**, read by excerpt: where something lives, who calls it, how
   repos talk to each other. Find files and content with Glob and Grep. Main clones under
   `~/Projects` are read-only and on whatever branch they're on. In a `spec` session, look at
   another branch, tag or commit with `ct spec repo checkout <repo> <ref>`: an
   exploration clone in the session.
   `ct spec repo fetch` gets the latest remote branches, and `ct spec repo ls`
   shows what's checked out. The clones
   are yours to experiment in: build, run tests, add debug output, or try a
   change to confirm a theory. **Never commit or push** (both are denied).
   The edits stay in the session, and `ct spec repo checkout --force` discards
   them. A fix worth keeping becomes a ticket (below), not a commit.
3. **Atlassian, read-only and free tools only:** Jira issues, Bitbucket PRs,
   commits and repos, when history or ownership matters. Never use unified
   `search` or the Teamwork Graph tools, which cost Rovo credits.

Answer concisely. Cite the vault notes you used as wikilinks, and the code
as `path:line` with the repo slug. Say what the answer rests on: notes (may
be stale) or the code itself (current on the base branch).
Where they disagree, trust the code, and say which note is out of date.

## Unknowns: triage them

When neither the vault nor a quick look settles it:
- **Investigate:** trace it through the code, check the repo's
  history (`git log` in its main clone), and the related tickets and PRs.
- **Say what couldn't be determined,** and what would settle it: who to
  ask, what to run, which environment to check. Never guess.
- If it turns into work (a bug, a missing feature, a risk, something that
  needs a deeper investigation), create a ticket for it (next section).

## Issues become tickets

When you find something that should be investigated or fixed, file it as a
**manual ticket**, so it enters the ticket workflow (`ct start <ID>`). When the user wants it
at a ticket source instead (e.g. Jira), run `/tickets:new-ticket`.
1. **Check first** that it isn't already tracked: search `tickets/` by repo
   slug and keywords, and Jira for synced tickets. If it is, add what you
   found to your answer and an `inbox/` draft instead.
2. A spec session has no current ticket: ask the user first (AskUserQuestion, per issue): a new
   ticket, or no ticket. Never run `ct new` without that answer. If yes, run `ct new --type <bug|investigation|dev|chore> "<summary>"`. It
   files into this session's vault and prints the new note,
   `tickets/manual/MAN-<n>/MAN-<n>.md`.
3. **Fill it in:**
   - `## Description`: what you found and why it matters, with the
     evidence (`<slug>:path:line`, call paths, commits, how to reproduce).
   - `## Acceptance criteria`: what "resolved" means.
   - `## Context / links`: related notes and tickets as wikilinks.
   - Frontmatter: `projects: [<slug>, ...]`, `priority` if clear, the
     dependency fields if it blocks or relates to other tickets, and the
     tag `from-spec`.
   - A review task for the user under `## Follow-ups`, so it shows up in the Tasks
     views (`inbox.md`, the ticket's status file once started):
     `- [ ] Review this ticket filed by spec: check the findings, scope, type
     and priority, then ct start it, ignore it, or close it #follow-up
     ➕ <today>` (date from `date +%F`).

   Write only that note. The ticket is the user's from then on.
4. **Tell the user** what you filed. In an interactive session, if it's a
   judgment call (minor, maybe intended), ask before filing.

## Record what you learned: drafts in `inbox/`

Don't edit `projects/`, `knowledge-base/` or `references/` directly. Other
sessions may be writing there, and only `/tickets:save` writes them, under the vault
lock. Instead, for each durable fact that the vault lacked or had wrong:
- Write a draft note in `<vault>/inbox/`, using the matching template
  (`feature.md` as an index, `sequence.md` with a mermaid diagram for a flow across a group,
  `decision.md`, `knowledge.md`, `reference.md`, `project.md` for a repo
  with no notes yet).
- **Filename:** final, vault-unique and kebab-case, so `/tickets:save` can move it
  as-is.
- **Frontmatter:**
  - `target:` where it belongs (`projects/<slug>/features`,
    `projects/<group>/sequences` for a flow across a group's members,
    `knowledge-base/<topic>`, `references`, ...). A new dependency between
    projects is an `update-of` the dependent's index note (`depends-on`
    and its `## Depends on` row)
  - `update-of: <note>` when it corrects or extends an existing note
  - `session: <yyyy-MM-dd>-<topic>`, so `/tickets:save` knows which drafts are this
    session's
  - the usual frontmatter from `AGENTS.md`
- Link the drafts as usual: at least 2 wikilinks, by bare note name.
- Tell the user what you drafted, and remind them that `/tickets:save` promotes it.

Keep the drafts to facts worth keeping: how things work, why they're built
that way, which versions and projects are involved, and gotchas. Leave out
the conversation itself.

## Sessions outside tickets
`ct claude` in a repo gives the session the default vault (`ct vault default`) as its knowledge
base, and its system prompt names the vault and the repo's `projects/<slug>/`; a plain `claude`
session knows nothing of the vault. This skill answers from the vault, then the code, and records new or corrected facts as drafts in `inbox/` (`target:`, `session:`).
`/tickets:save` in that session writes the project log and promotes the session's drafts;
`/tickets:recall` works there too. Sessions outside tickets (a repo session, `ct spec`) write only new
drafts in `inbox/`, each under its own name, until `/tickets:save`; a `spec` session may also create
a manual ticket (`ct new`, tag `from-spec`) once the user has said yes, and from then on such a
ticket (`from-spec` or `from-ticket`) is the user's, like any manual ticket.

## Planning a change (`/tickets:spec plan <feature>`)
Only in a main session with AskUserQuestion (a `spec` session, `ct claude`, a ticket's PO). Output is
tickets carrying spec text and a vault draft, never commits to a repo.
1. **Where specs live:** per repo, `ct spec location <slug>`. Neither `specs/` in the repo nor a
   `specs:` field in its project note: ask once (AskUserQuestion: repo, or vault only) and write the
   field. A vault-located spec (`specs: vault`) is edited as a copy in the ticket folder
   (`tickets/<source>/<ID>/specs/<slug>/<capability>/spec.md`), made with
   `ct spec start <ID> <slug> [<capability>…]`, never by hand (it snapshots `projects/` for
   promote); `/tickets:save` promotes it to
   `projects/<slug>/specs/`. Nothing spec-related goes into such a repo.
2. **Clarify** with `tickets:clarify`, per feature, then per project and epic: rounds of at most 4
   questions, the decisions that change most first, no cap on rounds. A skipped question stays a
   `[NEEDS CLARIFICATION: ...]` line under its requirement.
3. **Project and epic:** ask which project (Jira project) the work goes in. Find the epic or
   confirm creating one; the epic is created before any child (`/tickets:new-ticket`, epic flow).
4. **One child per capability or slice.** Its description carries a `## Spec change` heading:
   the capability, its requirements (`### REQ-<PREFIX>-<n> · <title>`, `status: proposed`, SHALL
   text) each with `#### Scenario:` WHEN/THEN blocks, and the interface changes (provides and
   consumes, naming the other repos). Self-contained for teammates. New IDs come from the base.
   A requirement still carrying a `[NEEDS CLARIFICATION]` marker blocks filing its child: ask the
   question again, or leave that child unfiled and say so. Filing goes through `/tickets:new-ticket`
   (ticket writes use the session's MCP tools, never a Go client).
5. **`spec-draft.md`:** right after each child is filed, write or update
   `tickets/<source>/<ID>/spec-draft.md` (the epic's folder) with what doesn't fit a description:
   rejected options, why, open questions, cross-repo notes, the filed children. `ct sync` leaves
   it alone; the ticket's kickoff reads it first.

## Analyze (`/tickets:spec analyze [<slug>...]`)
Read-only. Without slugs, the repos the session names.
1. Run `ct spec check [<slug>...] --json` (duplicate IDs, malformed blocks, requirements with
   markers, `implemented` requirements no test cites, provides/consumes rows with no match in the
   other repo's spec; exit 1 means findings). Keep each finding with its file and ID.
2. Then read code for what the command can't see: specs that no longer match the code (a
   requirement or scenario the code contradicts or no longer does), `implemented` requirements
   whose cited code moved. Use Grep, by excerpt; cite `<slug>:path:line`.
3. Offer each finding as a ticket (AskUserQuestion, one question listing them: file all, pick,
   none) through `/tickets:new-ticket`, or `ct new` in a session with no ticket and the user's yes
   (tag `from-spec`). Never edit a spec in analyze.

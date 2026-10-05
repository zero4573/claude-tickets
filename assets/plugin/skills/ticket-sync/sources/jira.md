# Source adapter: jira

Jira Cloud through the Atlassian Rovo MCP server, under the `mcp` name in
`.sources.json` (normally `atlassian`): the session gets it from its claude
command, and the `ct sync` command from `$CLAUDE_TICKETS_MCP_CONFIG`. The token is read-only: `read:jira:agent-interface`,
`search:jira:agent-interface`, `read:me` and `read:account`. Without the last
two, every Jira call fails with "Failed to fetch accessible products: 401". The vault note
`references/atlassian-rovo-mcp-setup.md` covers the setup.

The `ct sync` command implements this adapter itself, without a model
(`internal/jira` in claude-tickets: planner, changelog-driven patches, the
same mapping and layout), and hands only HTML-only parts to
the skill. Change both together.

## Querying
- **Site:** use the source's optional `site` from `.sources.json` (e.g.
  `yourcompany.atlassian.net`) as the `cloudId` argument of every tool call,
  if it's set. The Rovo tools accept a site hostname there as well as a UUID.
  Otherwise call `getAccessibleAtlassianResources` once (it needs the
  token's `read:me` / `read:account` scopes) and keep the resources whose
  `products` include `jira`:
  - **exactly one:** use its `cloudId` for every call;
  - **several:** stop with an error listing their URLs, and ask the user to
    set `"site"` for the jira source in `tickets/.sources.json` (never pick
    one yourself);
  - **none:** stop and report that the token can't reach any Jira site.
- **Auth errors:** a 401 or 403 from a search or issue call means the token
  is wrong, expired, or not granted to that site. Stop at the first one and
  report it, with the site and the call that failed. Don't retry with other
  sites or tools.
- **Search:** run the JQL search tool (e.g. `searchJiraIssuesUsingJql`) with
  the source's `query`. The default is
  `assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC`.
  Page with `nextPageToken`.
- **Step 2 (cheap listing):** request `fields: ["updated", "status"]` with
  `view: "full"`, `maxResults: 100`. The default `compact` view drops
  `updated` and the status category even when asked for, and the sync needs
  both.
- **Full ticket:** fetch it with the issue tool (e.g. `getJiraIssue`) using
  `view: "evidence"`, which includes issue links. Comments come from
  `listJiraIssueComments` (find it with `discover` and run it through
  `executeRead`). Use
  the fields summary, issuetype, status, priority, assignee, reporter,
  fixVersions, components, labels, parent, issuelinks, description, the
  last 5 comments, and attachment names.
- **By ID (step 4):** JQL `key in (A-1, B-2, ...)` in batches of 50, with
  `fields: ["updated", "status", "assignee"]` and `view: "full"`.

## Mapping
| Note field | From |
|---|---|
| ID / `source-id` | issue key (`PROJ-12`). `idPrefix` is normally empty for Jira |
| `title` | the ID |
| `summary` | summary |
| `source-url` | `https://<site>/browse/<key>` |
| `source-type` | issuetype name |
| `source-status` | status name |
| `source-status-category` | status category key: `new` → `todo`, `indeterminate` → `in-progress`, `done` → `done` |
| `source-priority` | priority name |
| `source-assignee` | assignee display name |
| `source-fix-versions` | fixVersions names (a list) |
| `source-updated` | `updated`, verbatim |
| `ticket-type` | Epic (or any issue type at hierarchy level 1) → `epic`; Bug → `bug`; Spike, Investigation or Research → `investigation`; Story, Task, Improvement, New Feature or Sub-task → `dev`; else `chore` |
| `parent` | the `parent` field's key, `"[[KEY]]"` (this covers both epic parents and a sub-task's parent) |
| `blocked-by` | issuelinks with an `inwardIssue` whose link type's inward wording is "is blocked by" (link type `Blocks`) |
| `blocks` | issuelinks with an `outwardIssue` of link type `Blocks` |
| `related` | every other issuelink (relates to, duplicates, clones, causes, ...) |
| `blocked` | `true` if any `blocked-by` issue's `fields.status.statusCategory.key` isn't `done` (issuelinks carry the linked issue's status, so no extra calls) |
| extra tag | the Jira project key in kebab-case, e.g. `proj` |

In the source block, the heading is `## Source (Jira)`. Add a
`Components / labels` row, and render `Parent` and `Links` from parent and
issuelinks (link type wording, then `[[<key>]]`). Convert the description
and comments from Atlassian Document Format to Markdown.

## Epics
- **Children:** JQL `parent = <EPIC-KEY> ORDER BY status, key`, with no
  assignee filter, requesting `fields: ["summary", "status", "assignee",
  "issuetype"]` and `view: "full"`, paged. These children cost one search
  per epic per refresh.
- **Parent epics:** when a synced ticket's `parent` has issue type Epic and
  no note, fetch it with `getJiraIssue` like any ticket. The parent's type
  is in the child's `parent.fields.issuetype`.

## Legacy notes
Notes written by the old `jira-sync` skill have `jira-*` frontmatter and
`<!-- jira:start -->` / `<!-- jira:end -->` markers. Migrate each one you
touch, or that step 4 finds:
- Rename `jira-key` → `source-id`, `jira-url` → `source-url`,
  `jira-status` → `source-status`, `jira-status-category` →
  `source-status-category` (mapping it as above), `jira-type` →
  `source-type`, `jira-priority` → `source-priority`, `jira-assignee` →
  `source-assignee`, `jira-fix-versions` → `source-fix-versions`,
  `jira-updated` → `source-updated`.
- Add `source: jira`.
- Replace the old markers with the `source:` markers and the `## Jira`
  heading with `## Source (Jira)`.
- List the ticket under `migrated`.

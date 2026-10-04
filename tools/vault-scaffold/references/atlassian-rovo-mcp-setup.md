---
title: atlassian-rovo-mcp-setup
created: 2026-10-02
updated: 2026-10-03
status: active
type: reference
source: https://developer.atlassian.com/cloud/rovo-mcp/guides/configuring-authentication-via-api-token/
tags: [reference, jira, bitbucket, mcp]
---
# atlassian-rovo-mcp-setup

How Claude sessions reach Jira (the `jira` ticket source) and Bitbucket (PR
feedback, related repos at kickoff, `kb`): the Atlassian Rovo MCP server,
best behind a local proxy that holds the token, so credentials stay out of
the sessions. Used by `ticket-sync` to fill `tickets/`. See [[AGENTS]] and the [[tickets.base|ticket dashboard]].

## Setup (one time)
1. **Admin:** an org admin enables API-token authentication for the Rovo
   MCP server (Admin → Rovo / AI settings → Rovo MCP server →
   Authentication). Your account needs Jira access and Browse Projects on
   the relevant projects.
2. **Token:** id.atlassian.com → Security → API tokens → *Create API token
   with scopes*, app **Jira**, with only these scopes:
   - `read:jira:agent-interface`
   - `search:jira:agent-interface`
   - `read:me` and `read:account` (your own profile and the sites you can
     reach; without them every Jira call fails with "Failed to fetch
     accessible products: 401")

   For Bitbucket (`/tickets:pr-feedback`, the kickoff's related-repo search, `kb`),
   add the Bitbucket scope to the same token: `read:bitbucket:agent-interface`
   (repos, pull requests, comments). The token screen offers only that and
   `write:bitbucket:agent-interface` for Bitbucket; leave the write one out.

   No `write:`/`delete:`/`manage:`/`admin:` scopes. Tokens expire after at
   most a year.
3. **Store the token** where your MCP proxy reads it (e.g. a password
   manager item), or, without a proxy, in the MCP config itself (below).
4. **Register** the server as `atlassian` (the `mcp` of the `jira` source in
   `tickets/.sources.json`), with Basic auth (`base64(email:token)`):
   - for sessions: in the MCP config your `claude` command uses
   - for `ticket-sync`: in the standard MCP config named by
     `CLAUDE_TICKETS_MCP_CONFIG`, e.g.
     `{"mcpServers": {"atlassian": {"type": "http", "url": "http://127.0.0.1:<port>/atlassian"}}}`
     behind a proxy, or `{"type": "http", "url": "https://mcp.atlassian.com/v2/mcp", "headers": {"Authorization": "Basic …"}}`
     without one.
5. **Site (optional):** ticket-sync finds your Jira site itself, through
   `getAccessibleAtlassianResources` (hence the `read:me` / `read:account`
   scopes). Only if the token can reach several Jira sites, set `"site"` on
   the `jira` source in `tickets/.sources.json` (e.g.
   `yourcompany.atlassian.net`); it's then passed as the `cloudId` on every
   call.
6. **Check:** `ticket-sync` connects, or `vault-configure --section sources`
   lists `atlassian` among the servers it finds.

## Behaviour and gotchas
- Rovo filters its tools by the token's scopes. With read scopes only, the
  server is read-only, so agents can never change Jira or Bitbucket.
- A legacy (unscoped) API token only exposes Teamwork Graph tools, with no
  Jira tools at all.
- Personal tokens use **Basic** auth (`base64(email:token)`). Bearer is only
  for service-account API keys.
- The old `/v1/sse` endpoint was retired. Use `/v2/mcp`.
- **"You don't have permission to connect via API token":** the org hasn't
  allowed API-token auth for you. An org admin enables it in step 1.
- **401 on a Jira call:** a scoped token is created for one site (picked when
  you create it). A 401 means the site used (the `site` in `.sources.json`, or
  the one found) isn't that site, the token expired, or the stored token
  is a different one. Scoped tokens
  only work through the API gateway, not the site URL. Check on the host with
  the cloud ID (`https://<site>/_edge/tenant_info`):
  `curl -s -u "<email>:<token>" https://api.atlassian.com/ex/jira/<cloudId>/rest/api/3/myself`.
  The reply tells you which: `Client must be authenticated` means the email
  and token pair is wrong; `scope does not match` or a 403 means valid
  credentials but missing scopes; your user JSON means the credentials are fine.
- **`Failed to fetch accessible products: 401`** on every Jira call: Rovo's
  internal product check rejected the credentials. Run the curl above to see why.
- **403 on `getAccessibleAtlassianResources` / `atlassianUserInfo`:** the token
  lacks `read:me` / `read:account`. Recreate it with them (or set `site`).
- **401 or 403 on a Bitbucket call:** the token lacks
  `read:bitbucket:agent-interface`, or your account can't read that
  workspace. `/tickets:pr-feedback` and the kickoff stop and report it rather than
  retry.

## Cost
- **Free:** single-product lookups, which is everything the workflow uses:
  Jira JQL search, `getJiraIssue`, and Bitbucket PRs, comments and repos.
- **Costs Rovo credits:** unified `search` and the Teamwork Graph tools
  (`getGraphContext`, `getGraphObject`, `addGraphContext`,
  `getTeamworkGraphContext`), at 1-10 credits per call. Overage is billed by default from 2026-12-03, at $0.01 per credit.
- **Guards:** deny these tools in your Claude settings, and in the session
  settings claude-tickets merges into every ticket session
  (`CLAUDE_TICKETS_SESSION_SETTINGS`, which the headless `ticket-sync` run
  gets too); the claude-tickets README has the rules. The vault's [[AGENTS]] ("Cost guard") tells every
  skill and role agent never to run them through `executeRead`, which the
  settings can't block by operation name.

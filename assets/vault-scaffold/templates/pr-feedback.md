---
title: {{title}}
created: {{date}}
updated: {{date}}
status: active
type: pr-feedback
threads: {}
tags: [pr-feedback]
---
# {{title}}

Review feedback on the open PRs for [[TICKET-ID]], written by `/tickets:pr-feedback`.
Each row is one review thread: what was asked, what was done in the
worktree (uncommitted until you commit), and a reply ready to paste into
Bitbucket. `threads` in the frontmatter records each thread's last update, so
the next run only looks at new or changed threads. Project: [[provider-owner-repo]].

## PR #<n>: <title>
<link>

| Thread | Where | Reviewer | Ask | Outcome | Draft reply |
|---|---|---|---|---|---|

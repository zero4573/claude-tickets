---
title: {{title}}
created: {{date}}
updated: {{date}}
status: active
type: feature
projects: []
tickets: []
tags: [feature]
---
# {{title}}

An index of one feature across tickets and versions. What the feature does
is the spec's job (the requirements and scenarios under `specs/`, in the
repo or at `projects/<slug>/specs/`); this note only points to it.

## Tickets
- [[TICKET-ID]]: what it added

## Versions
| Project | Change | Introduced in |
|---|---|---|
| [[provider-owner-repo]] | new endpoint `POST /orders/sync` | 2.4.0 |

## Compatibility
What older versions of the projects that depend on these changes see. In a
group, also a row per project in the group's `<group>-compatibility` note.
n/a for a standalone project.

## Specs
Links to the spec sections: `<slug>/<capability>` and the requirement IDs
(`REQ-ORD-14`).

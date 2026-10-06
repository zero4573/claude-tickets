---
title: {{title}}
created: {{date}}
updated: {{date}}
status: active
type: sequence
projects: []
tickets: []
tags: [sequence, data-flow]
---
# {{title}}

What triggers this flow and what it achieves.

```mermaid
sequenceDiagram
    participant A as project-a
    participant B as project-b
    A->>B: POST /thing {id}
    B-->>A: 202 Accepted
```

## Steps and contracts
1. `project-a` → `project-b`: payload, auth, errors, retries

## Versions
| Project | From version |
|---|---|
| [[provider-owner-repo]] | 2.4.0 |

Feature: [[feature-note]] · Ticket: [[TICKET-ID]]

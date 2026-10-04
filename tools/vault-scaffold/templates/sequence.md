---
title: {{title}}
created: {{date}}
updated: {{date}}
status: active
type: sequence
services: []
tickets: []
tags: [sequence, data-flow]
---
# {{title}}

What triggers this flow and what it achieves.

```mermaid
sequenceDiagram
    participant A as service-a
    participant B as service-b
    A->>B: POST /thing {id}
    B-->>A: 202 Accepted
```

## Steps and contracts
1. `service-a` → `service-b`: payload, auth, errors, retries

## Versions
| Service | From version |
|---|---|
| [[provider-owner-service-a]] | 2.4.0 |

Feature: [[feature-note]] · Ticket: [[TICKET-ID]]

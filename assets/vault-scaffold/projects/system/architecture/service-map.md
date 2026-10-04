---
title: service-map
created: 2026-10-02
updated: 2026-10-02
status: active
type: permanent
tags: [system, architecture, service-map]
---
# service-map

Which services talk to which, and how. `/tickets:save` adds an edge for each new
interaction a ticket introduces (`A -->|REST: POST /x| B`), and each node
should have a project note in `projects/<slug>/` (slug = `<provider>-<owner>-<repo>`). Part of [[system]], with
versions in [[compatibility-matrix]].

```mermaid
graph LR
    %% service-a -->|REST: GET /orders| service-b
    %% service-b -->|Kafka: order.created| service-c
```

## Services
| Service (slug) | Main clone | Project note |
|---|---|---|

---
capability: posting
prefix: LED
---
# Posting

## Purpose
Posting.

## Requirements

### REQ-LED-1 · Post
status: proposed
The system SHALL post an entry.

#### Scenario: post
- WHEN an order arrives
- THEN an entry is posted

## Interfaces

### Provides
| Interface | Kind | Where | With |
|---|---|---|---|

### Consumes
| Interface | Kind | Where | With |
|---|---|---|---|
| POST /orders | http | api | acme-billing |

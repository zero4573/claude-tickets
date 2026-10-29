---
capability: order-sync
prefix: ORD
---
# Order sync

## Purpose
Keeps orders in step with billing.

## Requirements

### REQ-ORD-14 · Sync retries
status: implemented            # proposed | implemented | inferred
The system SHALL retry a failed order sync up to 3 times with backoff.

#### Scenario: transient failure
- WHEN the billing API returns 503
- THEN the sync is retried after 1s, 2s and 4s
- AND the order is marked failed after the third retry

### REQ-ORD-15 · Inferred cancel
status: inferred
evidence: src/cancel.go:10, src/cancel.go:40
tests: src/cancel_test.go:5
The system SHALL cancel an order on request.

#### Scenario: cancel
- WHEN a user cancels
- AND the order is open
- THEN the order is cancelled

## Interfaces

### Provides
| Interface | Kind | Where | With |
|---|---|---|---|
| POST /orders/sync | http | api | acme-billing |

### Consumes
| Interface | Kind | Where | With |
|---|---|---|---|

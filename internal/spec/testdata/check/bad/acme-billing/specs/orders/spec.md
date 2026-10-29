---
capability: orders
prefix: ORD
---
# Orders

## Purpose
Orders.

## Requirements

### REQ-ORD-1 · Create
status: implemented
The system SHALL create an order.

#### Scenario: create
- WHEN a user orders
- THEN an order exists

### REQ-ORD-2 · Cancel
status: proposed
The system SHALL cancel an order.
[NEEDS CLARIFICATION: who may cancel?]

#### Scenario: cancel
- WHEN a user cancels
- THEN the order is cancelled

### REQ-ORD-3 · Broken
status: implemented
The system must refund.

## Interfaces

### Provides
| Interface | Kind | Where | With |
|---|---|---|---|
| POST /orders | http | api | acme-ledger |
| GET /refunds | http | api | acme-ledger |
| GET /ghost | http | api | acme-nowhere |

### Consumes
| Interface | Kind | Where | With |
|---|---|---|---|

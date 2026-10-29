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

#### Scenario: cancel
- WHEN a user cancels
- THEN the order is cancelled

## Interfaces

### Provides
| Interface | Kind | Where | With |
|---|---|---|---|
| POST /orders | http | api | acme-ledger |

### Consumes
| Interface | Kind | Where | With |
|---|---|---|---|

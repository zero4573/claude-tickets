---
capability: orders
prefix: ORD
---
# Orders

## Purpose
Orders.

## Requirements

### REQ-ORD-1 · Create
status: proposed
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

### REQ-ORD-3 · Archive
status: proposed
The system SHALL archive an order.

#### Scenario: archive
- WHEN a month passes
- THEN it is archived

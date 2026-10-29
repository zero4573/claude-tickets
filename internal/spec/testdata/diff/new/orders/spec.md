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
The system SHALL cancel an order within a day.

#### Scenario: cancel
- WHEN a user cancels
- THEN the order is cancelled

### REQ-ORD-4 · Refund
status: proposed
The system SHALL refund a cancelled order.

#### Scenario: refund
- WHEN an order is cancelled
- THEN the buyer is refunded

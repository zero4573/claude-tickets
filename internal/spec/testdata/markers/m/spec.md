---
capability: m
prefix: MK
---
# M

## Requirements

### REQ-MK-1 · Open
status: proposed
The system SHALL expire sessions.
[NEEDS CLARIFICATION: after how long?]
[NEEDS CLARIFICATION: per tenant?]

#### Scenario: expiry
- WHEN idle
- THEN expired

### REQ-MK-2 · Closed
status: proposed
The system SHALL log out.

#### Scenario: logout
- WHEN asked
- THEN done

---
capability: bad
prefix: BAD
---
# Bad

## Requirements

### REQ-BAD-0 · Zero id
status: proposed
The system SHALL x.

### REQ-BAD-1 · No status
The system SHALL y.

#### Scenario: s
- WHEN a
- THEN b

### REQ-BAD-2 · Bad status
status: done
The system SHALL z.

#### Scenario: s
- WHEN a
- THEN b

### REQ-BAD-3 · No shall
status: proposed
The system will do it.

#### Scenario: s
- THEN b first

### REQ-BAD-3 · Duplicate
status: proposed
The system SHALL again.

### Not a requirement
text

### REQ-OTH-4 · Wrong prefix
status: proposed
The system SHALL w.

#### Scenario: s
- WHEN a
- THEN b

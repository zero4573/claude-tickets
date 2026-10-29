Summary text.

## Spec change

### billing · order-sync

### REQ-ORD-20 · No double charge
status: proposed
The system SHALL charge an order once.

#### Current
Retries charge twice.

#### Expected
A retry never charges twice.

#### Unchanged
Failed orders stay failed.

#### Interfaces
| Interface | Kind | Where | With |
|---|---|---|---|
| POST /charge | http | api | acme-billing |

## Other
not part of it
### REQ-ORD-99 · ignored

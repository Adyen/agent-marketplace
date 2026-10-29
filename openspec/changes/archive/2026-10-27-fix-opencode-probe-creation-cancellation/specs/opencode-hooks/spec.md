## ADDED Requirements

### Requirement: Offline native probe cancellation retains container ownership

Cancelling the offline OpenCode compatibility probe during container creation, including immediate cancellation before startup bookkeeping completes, MUST preserve cleanup ownership until creation completes or reaches its existing deadline. Direct and process-group cancellation MUST exit nonzero, remove only the container created by that invocation, and MUST NOT start the cancelled container. An unrelated container whose name collides MUST NOT be removed.

#### Scenario: Probe cancellation waits for owned container creation

- **WHEN** direct or process-group cancellation reaches the probe while container creation is pending, including immediately after creation starts
- **THEN** the probe MUST retain its nonzero cancellation outcome and clean up the owned container once its ID is available
- **AND** it MUST NOT start the cancelled container or remove a container it did not create

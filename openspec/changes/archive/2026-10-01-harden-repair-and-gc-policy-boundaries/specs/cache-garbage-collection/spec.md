## ADDED Requirements

### Requirement: Collection preflight does not authorize stale policy

Real cache collection SHALL revalidate the local configuration and deployed acquisition evidence used to determine reachability after preflight and before deletion. A changed or newly deployed input SHALL produce a generation conflict without deleting cache objects or acquiring resources over the network.

#### Scenario: Organization acquisition lock appears after preflight

- **WHEN** an organization acquisition lock is deployed after collection preflight used its absence
- **THEN** collection SHALL report a generation conflict, delete no cache objects, and make no network requests

#### Scenario: Configuration changes after preflight

- **WHEN** configuration changes after collection preflight computed reachable objects
- **THEN** collection SHALL report a generation conflict and delete no cache objects

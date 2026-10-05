## ADDED Requirements

### Requirement: Ordinary reloads retain the current view until final assessment

Outside initial startup, an ordinary reload SHALL retain the last accepted resource list while resolution is in progress. It SHALL replace that view only with the final authoritative snapshot and SHALL NOT display provisional configured rows or intermediate configuration and inventory states. Ordinary reloads SHALL NOT perform startup-only provisional catalog ingestion, provisional harness-default reads, or browse-only endpoint identity discovery; authoritative resolution and assessment SHALL remain unchanged.

#### Scenario: Ordinary reload keeps the current view until final assessment

- **WHEN** an ordinary reload starts while an accepted resource list is displayed and then resolves a different resource list
- **THEN** the accepted list SHALL remain displayed until the final assessed snapshot is accepted, after which the authoritative rows and drift SHALL replace it together

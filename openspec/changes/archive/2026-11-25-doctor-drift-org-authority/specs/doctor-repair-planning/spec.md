# Spec Delta

## MODIFIED Requirements

### Requirement: Repair reconciles harness drift without changing acquisition locks

Repair SHALL plan harness drift that sync can reconcile as a safe step, after configuration and acquisition-lock steps and before shared-aggregate ownership repair. The step SHALL reconcile drift through sync using existing acquisition locks and the same verified transient organization Marketplace evidence used for diagnosis when no organization acquisition lock is deployed. It SHALL NOT change, create, or refresh any acquisition lock. A present invalid organization acquisition lock SHALL NOT be replaced by transient evidence. An unmanaged shared-aggregate collision SHALL be left to shared-aggregate ownership repair.

#### Scenario: Drift repair leaves acquisition locks byte-identical

- **WHEN** a materialized harness file has drifted from the selected resources and repair runs the drift step
- **THEN** a subsequent sync check SHALL report no drift
- **AND** every acquisition lock SHALL be byte-identical to its state before repair

#### Scenario: Drift repair uses verified transient organization evidence

- **GIVEN** a valid online organization Marketplace configuration with no deployed acquisition lock, harness drift, and an exact shared-aggregate match eligible for adoption
- **WHEN** doctor plans and the user authorizes repair
- **THEN** repair SHALL clear the drift and continue to the adoption, without creating an organization acquisition lock or changing organization configuration or any writable acquisition lock
- **AND** standalone `sync --locked` SHALL still refuse the missing organization acquisition lock

#### Scenario: Drift repair refuses invalid organization evidence

- **WHEN** deployed organization acquisition evidence is invalid or transient organization Marketplace evidence cannot be verified
- **THEN** doctor SHALL NOT reconcile drift or adopt shared entries using that selection
- **AND** it SHALL NOT replace or publish organization acquisition evidence

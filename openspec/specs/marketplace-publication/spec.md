# marketplace-publication Specification

## Purpose
Defines expected-head validation and verified no-op retry behavior for legacy registry publication, preserving current resource assessments and transactional metadata integrity.

## Requirements

### Requirement: Legacy publication validates its expected head

A legacy registry publication MUST validate its expected effective registry head within the metadata transaction before changing current resource assessments or other publication metadata. A stale expectation, including an empty expectation after initialization, MUST return a conflict unless every requested metadata item is already current and the request is a verified no-op. Refusals MUST preserve all visible metadata. Publication-identity retries SHALL retain their existing contract. A legacy same-commit retry with a matching head expectation SHALL remain valid.

#### Scenario: Stale same-commit assessment is refused

- **GIVEN** a resource revision at the current registry commit has a reject assessment
- **WHEN** a legacy publication for that same commit supplies an allow assessment and an outdated or empty expected head
- **THEN** publication MUST return `store.ErrConflict`
- **AND** the current assessment, registry head and assessment history MUST remain unchanged

#### Scenario: Valid same-commit retry preserves the assessment

- **GIVEN** a legacy publication has initialized the registry head
- **WHEN** the same publication is retried with the matching current expected head
- **THEN** publication SHALL succeed and retain its current assessment

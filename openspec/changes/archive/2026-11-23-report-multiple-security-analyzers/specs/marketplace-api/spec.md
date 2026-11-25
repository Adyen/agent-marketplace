## ADDED Requirements

### Requirement: Advisory reports extend resource assessments without replacing existing fields

Resource assessment responses MUST retain existing built-in findings, analyzer version, and policy fields. If present, advisory reports MUST carry a stable tool identifier, applicable version, status, and bounded findings. They MUST be bound to the assessment identity. Pre-existing assessments without advisory reports MUST retain their existing fields and digests.

#### Scenario: Both resource assessment reads carry identical advisory evidence

- **WHEN** an assessment with advisory reports is fetched through revision details and through its assessment digest
- **THEN** both responses MUST carry the same report data and assessment digest
- **AND** changing advisory evidence MUST change assessment identity, not resource revision identity

#### Scenario: Historical assessment remains readable without rewritten identity

- **WHEN** a resource assessment published before advisory reporting is read by its former digest
- **THEN** it MUST remain valid and the read MUST NOT fabricate a scan result or rewrite any identity

### Requirement: Advisory findings are safe to expose

Public advisory reports MUST NOT expose raw scanner output, snippets, credentials, environment data, or local working paths. A finding location MUST refer only to a verified inspection projection path and MUST omit any range that cannot be mapped to the corresponding bytes.

#### Scenario: Unmapped and unsafe scanner evidence is not published

- **WHEN** a scanner returns unsafe free text, private paths, or a location that cannot be mapped to the inspection projection
- **THEN** only the verified and bounded public finding fields MUST be exposed
- **AND** the result MUST NOT link to or highlight unrelated content

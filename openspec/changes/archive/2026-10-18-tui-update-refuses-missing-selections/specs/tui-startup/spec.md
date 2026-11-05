## MODIFIED Requirements

### Requirement: Missing optional selections are warnings

When a non-required, non-collection selection names a resource that a successfully loaded registry does not provide, the TUI SHALL show the selection as an unavailable row with its not-found reason and a warning diagnostic, and SHALL still assess drift for the remaining resources. Such a selection SHALL NOT fail the snapshot's authoritative assessment, mark rows stale, or turn a sync, update, install, or toggle reload into a failure. Required or collection selections that are not found, and selections whose registry catalog failed to load, SHALL continue to fail the assessment. An update refusal for a selection that resolved at the previously locked revision and stops resolving at the new one happens before publication, as `resource-selection` requires, and is not a reload failure under this requirement.

#### Scenario: Optional missing selection keeps the assessment

- **WHEN** the final stage resolves a configuration in which a loaded registry lacks a non-required selected skill
- **THEN** the missing skill MUST render as an unavailable row carrying its not-found reason, with a warning diagnostic
- **AND** drift MUST be assessed for the other resources and the footer MUST NOT report drift unavailable
- **AND** a sync reload of that snapshot MUST complete as success with warnings rather than failing verification

#### Scenario: Unverifiable selections still fail the assessment

- **WHEN** a required selection is not found, or a selected resource's registry catalog fails to load
- **THEN** the final stage MUST NOT report drift as assessed and a reload of that snapshot MUST fail

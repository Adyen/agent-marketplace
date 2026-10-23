# model-catalog-audit Specification

## Purpose
Defines when nightly model catalog proposals may change files and publish review
pull requests, while preserving reviewed evidence baselines and visible audit
findings.

## Requirements

### Requirement: Proposals preserve reviewed evidence baselines

The catalog proposal command SHALL leave evidence baseline bytes and metadata
unchanged. Changed fingerprints SHALL remain review findings, not automatic
acknowledgements that upstream evidence has been reviewed.

#### Scenario: Evidence-only drift does not edit reviewed data

- **WHEN** a proposal report contains changed documentation fingerprints but no
  actionable catalog additions
- **THEN** the proposal SHALL report the changed evidence as requiring review
- **AND** it SHALL NOT update evidence baselines

#### Scenario: Catalog additions do not acknowledge evidence drift

- **WHEN** a proposal can add a missing model with an unambiguous provider mapping
  and the report also contains evidence drift
- **THEN** it SHALL add the model catalog stub
- **AND** it SHALL leave all evidence baseline bytes and metadata unchanged
- **AND** it SHALL retain evidence drift in the proposal output

#### Scenario: Manual candidates remain visible without file edits

- **WHEN** missing model candidates have no unambiguous provider mapping and
  documentation fingerprints have changed
- **THEN** the proposal SHALL list candidates for manual entry
- **AND** it SHALL NOT change catalog or baseline files

### Requirement: Nightly publication requires catalog changes

The nightly proposal workflow SHALL publish a branch and create or update a
pull request only when the proposal changes model catalog files. Evidence-only
findings SHALL remain visible in job summaries and the audit artifact.

#### Scenario: Nightly proposals require catalog changes

- **WHEN** a nightly proposal contains documentation-only drift, manual-only
  candidates, or only baseline file changes
- **THEN** the workflow SHALL NOT publish a proposal branch or create a pull request
- **AND** it SHALL retain the evidence alert in the job summary
- **BUT WHEN** the proposal adds a model catalog entry
- **THEN** the workflow SHALL publish the catalog change and create its review PR
- **AND** it SHALL commit only catalog files

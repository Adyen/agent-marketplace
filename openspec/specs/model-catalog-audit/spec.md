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

### Requirement: Gemini API documentation supplies a model inventory

The audit SHALL monitor the Gemini API model documentation page as a model inventory source for the gemini harness. The parser SHALL extract model identifiers from that page and SHALL fail closed when no identifier is found.

#### Scenario: Gemini API page inventory lists a model absent from the catalog

- **WHEN** the Gemini API model documentation fixture lists `gemini-4-argon` and `gemini.yaml` does not contain `gemini-4-argon`
- **THEN** the audit SHALL report `catalog.model-missing` for `gemini-4-argon`

#### Scenario: Gemini API page inventory lists a model present in the catalog

- **WHEN** the Gemini API model documentation fixture lists `gemini-4-argon` and the catalog contains that identifier
- **THEN** the audit SHALL NOT report `catalog.model-missing` for `gemini-4-argon`

#### Scenario: Gemini API page without identifiers fails closed

- **WHEN** the Gemini API model documentation page contains no model identifiers
- **THEN** the audit SHALL report `source.inventory-unparsed` naming the Gemini API model documentation source

### Requirement: Unparseable model inventories are reported by name

The audit SHALL report `source.inventory-unparsed` naming the source when a model inventory source's page yields no parseable model list. A source with no parseable model list SHALL NOT pass silently.

#### Scenario: Inventory page without a model list

- **WHEN** an inventory source returns a page that contains no parseable model list
- **THEN** the audit SHALL report `source.inventory-unparsed` naming that source
- **AND** the source status SHALL be `unknown`

#### Scenario: Inventory page with a model list

- **WHEN** an inventory source returns a page with a parseable model list
- **THEN** the audit SHALL NOT report `source.inventory-unparsed` for that source

### Requirement: Cursor CLI output supplies the Cursor model inventory

The scheduled audit SHALL capture `agent models` output from the Cursor harness CI image and pass it to `check`. The audit SHALL use the model identifiers parsed from the `<id> - <display name>` lines that follow an `Available models` header line (matched case-insensitively, with an optional trailing colon) as the `cursor-models` inventory. Lines before that header SHALL be ignored. Missing, empty, or unparseable output, including output without the header or without any `<id> - <display name>` line after it, SHALL report `source.inventory-unparsed` for `cursor-models`.

#### Scenario: Cursor CLI output lists model identifiers

- **WHEN** `check` receives Cursor CLI output with `<id> - <display name>` lines after an `Available models` header
- **THEN** the `cursor-models` inventory SHALL contain those identifiers and the audit SHALL compare them with `cursor.yaml`

#### Scenario: Cursor CLI output is missing or unauthenticated

- **WHEN** `check` receives no Cursor CLI output, or output without any `<id> - <display name>` line after an `Available models` header
- **THEN** the audit SHALL report `source.inventory-unparsed` for `cursor-models`

#### Scenario: Cursor CLI diagnostic lines before the header are ignored

- **WHEN** `check` receives Cursor CLI output with `<id> - <display name>`-shaped diagnostic lines such as `error - Authentication required` or `1 - Getting started` before the `Available models` header
- **THEN** the `cursor-models` inventory SHALL contain only the identifiers listed after the header
- **AND** the audit SHALL NOT report those diagnostic lines as models

#### Scenario: Cursor documentation is unparseable but CLI output parses

- **WHEN** the Cursor model documentation cannot be parsed and `check` receives Cursor CLI output that yields a `cursor-models` inventory
- **THEN** the audit SHALL report `source.parse-failed` for `cursor-models` with status `unknown`
- **AND** the audit SHALL NOT report `source.inventory-unparsed` for `cursor-models`
- **AND** the audit SHALL compare the CLI identifiers with `cursor.yaml`

### Requirement: Family pins are flagged when a newer member is observed

The audit SHALL report `catalog.family-pin-stale` when a family pin names a model that is not the newest observed member of that family in the same source's inventory. Versions SHALL be compared numerically component by component, with missing trailing components treated as zero; a family member matches when its identifier contains the family name followed by a version. The audit SHALL NOT edit catalogs.

#### Scenario: Factory Haiku pin trails the observed Haiku

- **WHEN** the Factory inventory observes `claude-haiku-5-5` and the `anthropic/haiku` family pin names `claude-haiku-4-5-20251001`
- **THEN** the audit SHALL report `catalog.family-pin-stale` for `anthropic/haiku`

#### Scenario: Family pin is the newest observed member

- **WHEN** the family pin names the newest observed member of its family
- **THEN** the audit SHALL NOT report `catalog.family-pin-stale`

### Requirement: Devin model inventory is covered by the catalog

Every non-Fusion `model_uid` in the committed Devin CLI model snapshot SHALL be covered by a model or reasoning entry in `devin.yaml`. Fusion pairings are excluded from this requirement. The coverage test SHALL fail for each uncovered non-Fusion UID and SHALL list the uncovered UIDs.

#### Scenario: Every non-Fusion snapshot UID is covered

- **WHEN** every non-Fusion `model_uid` in the Devin CLI model snapshot matches a `devin.yaml` model or reasoning entry
- **THEN** the coverage test SHALL pass

#### Scenario: Non-Fusion snapshot UID is not covered

- **WHEN** a non-Fusion `model_uid` in the Devin CLI model snapshot matches no `devin.yaml` model or reasoning entry
- **THEN** the coverage test SHALL fail and SHALL list that UID

### Requirement: Report schema stays at version one

The audit report SHALL remain schema version 1, and `summary` and `propose` SHALL accept reports containing the finding codes introduced by this capability.

#### Scenario: Summary accepts new finding codes

- **WHEN** a version 1 report contains `source.inventory-unparsed` or `catalog.family-pin-stale` findings
- **THEN** `summary` SHALL render those findings without error

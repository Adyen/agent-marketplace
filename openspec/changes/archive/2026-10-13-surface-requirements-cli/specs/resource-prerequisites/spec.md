## MODIFIED Requirements

### Requirement: Status orders unmet prerequisites

The `status` `UNMET REQUIREMENTS` table SHALL list one row per unmet prerequisite with `RESOURCE` and `REQUIRES` as `<kind>:<name>`, `REASON` as its typed `not-found`, `excluded` or `source-not-configured` reason, and `FIX` as the existing redacted, POSIX-literal remediation. Rows SHALL be sorted by resource and then by prerequisite.

#### Scenario: Rows are sorted

- **WHEN** several selected resources have unmet prerequisites
- **THEN** status MUST print the rows sorted by `RESOURCE` and then by `REQUIRES`

#### Scenario: Status reports typed reasons safely

- **WHEN** selected resources have missing, excluded and unconfigured-source prerequisites, including a credential-bearing source hint
- **THEN** status MUST show the corresponding typed reasons and existing installation remedies
- **AND** credentials MUST be redacted before shell-literal formatting, and status MUST leave configuration, acquisition locks and materialized resources unchanged

## ADDED Requirements

### Requirement: List exposes prerequisite relationships

Configured `list` SHALL include a `VIA` column showing resolved collection provenance and `requires <kind>:<name>` provenance separately when both apply. `list --from` SHALL include a `REQUIRES` column listing valid declared prerequisite kind/name pairs in declaration order, with canonical CLI kind labels and empty cells for resources with no prerequisites. Existing available and selected rows, kind filtering, malformed prerequisite diagnostics and browse-only behavior SHALL remain intact. Registry-authored text SHALL retain terminal escaping, and credential-bearing source information SHALL be redacted before display.

#### Scenario: Configured list retains collection and requirement provenance

- **WHEN** selected resources have collection membership, requirement provenance or both and configured list runs
- **THEN** its VIA column MUST show the corresponding relationships without treating requirement provenance as a collection
- **AND** kind filtering MUST retain the matching rows

#### Scenario: Ad hoc list shows declarations without selecting them

- **WHEN** list --from browses resources with multiple valid prerequisite declarations, no declarations or malformed declarations
- **THEN** REQUIRES MUST show valid kind/name pairs in declaration order and remain empty for resources without valid declarations
- **AND** malformed prerequisite diagnostics MUST retain their existing non-quoting behavior, and browse MUST NOT write configuration, locks or materialized output

### Requirement: Doctor identifies unconfigured prerequisite hints

Doctor SHALL explicitly identify unmet prerequisites with reason `source-not-configured` as a source-hint configuration warning and present the existing install remediation including `--from`. The check SHALL remain warning-only, SHALL NOT configure or acquire the hinted source, and SHALL retain existing credential redaction, POSIX-literal argument formatting, malformed diagnostics and suppression for selected pending Marketplace providers. A prerequisite satisfied by a selected resource with the same kind and name SHALL NOT produce an unconfigured-source accusation.

#### Scenario: Doctor explains an unconfigured hint without mutation

- **WHEN** a selected resource has an unmet prerequisite whose credential-bearing source hint names no configured source and doctor runs
- **THEN** doctor MUST explicitly report the unconfigured source hint and the redacted literal install --from remedy with a successful ordinary exit status
- **AND** it MUST NOT acquire the hinted source or change configuration, locks or materialized output

#### Scenario: Doctor does not accuse satisfied or pending providers

- **WHEN** a prerequisite is satisfied by an actually selected kind/name provider from another source, or its selected Marketplace provider is pending usable local evidence
- **THEN** doctor MUST retain satisfaction or pending-provider suppression without an unconfigured-source accusation

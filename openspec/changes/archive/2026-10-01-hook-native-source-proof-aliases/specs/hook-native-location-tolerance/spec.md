## ADDED Requirements

### Requirement: Shared native entries retain proof for every contributing source

Ownership state for one owned native hook entry SHALL be able to record source-hook evidence for several selected resources that produce that entry. Each evidence tuple SHALL contain only a resource name and its version-1 source-hook fingerprint. Existing scalar resource-name and source-fingerprint evidence SHALL remain readable under the existing ownership-state version.

A legacy-writing client SHALL recognize a selected hook when its resource name and fingerprint match valid scalar evidence or a recorded evidence tuple and every matching owned native entry is present with clean current-byte proof. A matching tuple SHALL NOT establish current-byte ownership by itself or suppress a different, unproven resource, target, scope, or source revision. Missing or drifted matching entries SHALL suppress nothing for the affected selection.

#### Scenario: Several selected sources share one clean native entry

- **WHEN** two selected hook resources produce one native entry and clean ownership state records valid source-hook evidence for both resources
- **THEN** `sync`, `sync --locked`, and `sync --check` MUST recognize both resources without recreating legacy entries or reporting pending legacy writes
- **AND** they MUST leave the proven native entry and its ownership state unchanged

#### Scenario: Scalar source evidence remains compatible

- **WHEN** ownership state has a valid scalar resource name and source-hook fingerprint with no additional evidence tuples
- **THEN** the legacy-writing client MUST recognize that resource exactly as before
- **AND** it MUST NOT rewrite ownership state to add tuples merely by reading it

#### Scenario: Unproven contributor retains legacy behavior

- **WHEN** a shared native entry proves one selected resource but another selected resource has no matching resource-name and source-fingerprint tuple
- **THEN** the client MUST retain normal legacy output behavior for the unproven resource
- **AND** it MUST NOT change or adopt the native entry

#### Scenario: Partial shared proof cannot suppress a selection

- **WHEN** one selected resource has matching evidence on several recorded native entries and any matching entry is missing or drifted
- **THEN** the client MUST NOT suppress that resource's legacy output based on the remaining clean entries

### Requirement: Additional source evidence is validated and read-only

Additional source evidence SHALL be valid only on hook ownership entries. Resource names and source fingerprints SHALL satisfy the same non-secret validation rules as scalar evidence; empty names, absent or malformed fingerprints, wrong member types, and ambiguous modeled JSON fields SHALL NOT establish proof. Invalid ownership evidence SHALL fail closed for the affected target and scope under the existing native-tolerance refusal behavior, preserve native and legacy bytes, and leave independent valid targets able to continue. Diagnostics and logs SHALL NOT include rejected values or hook payloads.

#### Scenario: Invalid additional evidence refuses without mutation

- **WHEN** a hook's additional source evidence is malformed or ambiguous, or an MCP server ownership entry carries hook source evidence
- **THEN** the compatibility reader MUST refuse the affected target and scope with a stable coded error without changing its native file, legacy file, or ownership state
- **AND** the diagnostic and logs MUST NOT include the rejected values

#### Scenario: Read-only shared-proof checks leave state untouched

- **WHEN** `sync --check` checks complete or incomplete source evidence for a shared native hook entry
- **THEN** it MUST NOT create, update, chmod, or replace a native file, legacy file, or ownership-state file

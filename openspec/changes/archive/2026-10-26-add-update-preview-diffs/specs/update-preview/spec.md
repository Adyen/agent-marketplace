## Purpose

Lets consumers inspect proposed dependency and selected-resource updates without
changing their acquisition locks, configuration, or installed resources.

## ADDED Requirements

### Requirement: Update supports a composable preview

`update` SHALL accept `--dry-run` independently of `--include-diff`. A preview
SHALL use the same candidate selection, scope and targeting applicability, ref
bump eligibility, and policy checks as the corresponding applying invocation.
It SHALL summarize proposed configuration/ref changes, acquisition-lock changes,
and additions, modifications, removals, and source changes in the effective
selected-resource set. It SHALL identify the relevant scope, source, resource
kind/name, and available old and candidate exact revision identities.

Without `--include-diff`, the summary SHALL NOT include content hunks or require
historical file bodies solely to generate a diff. It SHALL state that no updates
were applied. A successful preview SHALL exit 0 both when changes are available
and when there is nothing to update; invalid arguments and preparation failures
SHALL exit nonzero.

#### Scenario: Preview summarizes a real update without content hunks

- **GIVEN** an exact acquisition lock selects a resource whose upstream definition or supporting file changes
- **WHEN** `update --dry-run` succeeds
- **THEN** it SHALL identify the affected resource, scope, source, and old/candidate revisions, summarize the proposed changes without content hunks, state that no updates were applied, and exit 0

#### Scenario: Preview with a diff uses the same candidates

- **GIVEN** fixed upstream inputs with a changed selected resource
- **WHEN** `update --dry-run` and `update --dry-run --include-diff` run from the same configuration and acquisition locks
- **THEN** they SHALL identify the same candidates and proposed changes
- **AND** only the latter SHALL include the resource diffs defined by `update-output`

#### Scenario: Preview preserves targeting and ref bump behavior

- **GIVEN** several configured sources and eligible ref bumps
- **WHEN** a valid `update --dry-run --bump` invocation specifies scope or Git registry/ref/path/resource targeting
- **THEN** its proposed candidates and ref edits SHALL match the corresponding applying invocation's flag applicability
- **AND** it SHALL neither widen the update selection nor propose edits to refs owned by included fragments

#### Scenario: No update is a successful preview

- **GIVEN** current acquisition locks, unchanged upstream resources, and no eligible ref edits
- **WHEN** `update --dry-run` runs with or without `--include-diff`
- **THEN** it SHALL report that no versioned update is available, state that no updates were applied, print no content hunks, and exit 0

### Requirement: Preview preserves consumer and installation state

A preview SHALL NOT publish configuration or acquisition locks, create missing
writable consumer roots or acquisition locks, materialize or prune resources,
create backups, record resource inventory, or mutate ownership state. It SHALL
preserve the bytes, file modes, and modification times of existing protected
files, including installed content reachable through symlinks.

Network acquisition, disposable acquisition-cache writes, cache coordination,
private temporary preparation, and ordinary sanitized operational logging SHALL
remain permitted. These activities SHALL NOT refresh content behind an installed
symlink, modify an exact snapshot already used by an installation, or leave
attempt-owned temporary preparation files after success, failure, or cancellation.

#### Scenario: Preview preserves an existing installation in either materialization mode

- **GIVEN** an installed selected resource, existing config and acquisition locks, ownership state, backups, and inventory, with a real upstream content change
- **WHEN** a preview succeeds in either symlink or file materialization mode
- **THEN** it SHALL show the proposed update while preserving those files' bytes, modes, and modification times and creating no backups or inventory/ownership changes
- **AND** content reached through the installed symlinks SHALL remain unchanged

#### Scenario: Initial acquisition is previewed without creating writable roots

- **GIVEN** supported consumer configuration whose applying update would create a missing writable acquisition lock or another missing writable consumer root
- **WHEN** a preview prepares that initial acquisition
- **THEN** it SHALL identify the absence of a previously locked baseline and the proposed acquisition without creating the missing lock or root
- **AND** with `--include-diff` it SHALL label any full candidate-content display as initial acquisition, not as a comparison against previously installed content

#### Scenario: Cancellation preserves state and removes private preparation

- **GIVEN** a real candidate update requiring acquisition and temporary preparation
- **WHEN** preview preparation fails or is cancelled
- **THEN** it SHALL exit nonzero, preserve protected consumer and installation state, and leave no attempt-owned temporary preparation residue

### Requirement: Preview retains validation and recovery refusals

A preview SHALL validate every required consumer root/include tree and applicable
source policy before new acquisition or private config/lock preparation, following
`consumer-config-format`. It SHALL check for pending dependency transactions
without replay and refuse with `dependency.recovery-required` when recovery is
needed.

Preview SHALL retain the applying update's newly-missing-selection refusal. It
SHALL NOT describe a blocked update as applicable or turn a directly selected or
required resource's disappearance into an allowed removal.

#### Scenario: Pending dependency transaction is not replayed

- **GIVEN** a pending dependency transaction and newer upstream resource content
- **WHEN** `update --dry-run` runs with or without `--include-diff`
- **THEN** it SHALL refuse with `dependency.recovery-required` without replay, new acquisition, or changes to the journal, consumer state, or installed content

#### Scenario: Unsupported consumer input refuses before new work

- **GIVEN** an unsupported or versionless required consumer root, or an explicitly unsupported included fragment, and a real upstream update
- **WHEN** a preview runs
- **THEN** it SHALL return the file/field-specific refusal required by `consumer-config-format` before new acquisition or config/lock preparation
- **AND** protected existing state and missing writable roots/locks SHALL remain unchanged

#### Scenario: Newly missing selection blocks the preview

- **GIVEN** a selection that resolved at its previously locked revision but does not resolve at the candidate revision
- **WHEN** a preview runs
- **THEN** it SHALL retain the `resource-selection` refusal, remediation, and nonzero exit
- **AND** it SHALL neither present that selection as an allowed removal nor claim the blocked update was applied

### Requirement: Preview reports the limits of a versioned comparison

Preview SHALL describe proposed versioned dependency/resource changes, not a
complete materialization dry run or a guarantee of subsequent harness-write
success. It SHALL NOT include local installed-file drift in an upstream content
diff or report a collision or ownership check as completed when it was not.

Live local-directory sources and organization sources that intentionally have no
acquisition lock SHALL be identified as having no historical locked comparison,
not as unchanged. Organization-managed exact selections SHALL retain their
existing not-checked/not-updated reporting and authority.

Each preview SHALL be independent of a later update invocation. It SHALL NOT
claim to reserve revisions or grant approval to a later invocation.

#### Scenario: Live sources are not mistaken for unchanged versioned resources

- **GIVEN** a live local-directory source or an intentionally unlocked organization source alongside a versioned resource update
- **WHEN** a preview runs
- **THEN** it SHALL identify the source as lacking a historical locked comparison and limit its upstream diff to comparable versioned resources
- **AND** it SHALL state that the preview does not enumerate every subsequent materialization change

#### Scenario: Installed-file drift does not enter the upstream diff

- **GIVEN** a locally modified installed file and independently changed locked-to-candidate resource content
- **WHEN** `update --dry-run --include-diff` runs
- **THEN** its content comparison SHALL use the previously locked resource and candidate, not the installed file
- **AND** it SHALL not claim to have completed all harness-write or ownership checks

#### Scenario: Separate invocations may discover different revisions

- **GIVEN** upstream advances after a successful preview
- **WHEN** a later applying update prepares its candidates
- **THEN** it SHALL use that invocation's normal candidate-selection behavior rather than treating the previous preview as a reserved plan or approval

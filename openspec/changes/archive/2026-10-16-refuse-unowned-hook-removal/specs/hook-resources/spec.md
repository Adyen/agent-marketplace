## ADDED Requirements

### Requirement: Removal refuses unowned active native hook residue

Before `remove` publishes configuration or acquisition-lock changes, it SHALL
check native hook aggregate entries that would cease to be selected, including hooks
expanded from a removed collection or source. If a matching active native entry
lacks clean ownership proof, removal SHALL fail with `hook.remove-unowned`,
identify the harness target, resource and native file, explain that the hook
remains active, and suggest reviewing the file or explicitly repairing ownership
before retrying. It SHALL NOT report successful removal. Configuration,
acquisition locks, native hook files and ownership state SHALL remain unchanged.
An absent ownership-state root SHALL remain absent. Diagnostics SHALL NOT quote
hook commands, prompts, matchers or other executable payloads.

Full native-value equivalence MAY identify residue but SHALL NOT grant
ownership, adoption or deletion authority. Handwritten matches and missing or
corrupt ownership state SHALL retain fail-closed preservation. A native entry
still required by another selected hook SHALL NOT block deselection of one
contributor. Absent native entries and cleanly owned stale entries SHALL retain
ordinary removal behavior.

This aggregate preflight SHALL apply only to targets with a documented native
hook aggregate projection. Targets with file-based hook declarations instead
SHALL retain their ordinary file-ownership and removal behavior. The aggregate
preflight SHALL NOT inspect, adopt or rewrite their declaration or adapter files.

#### Scenario: Independent checkout refuses removal of a committed hook

- **WHEN** a teammate with a separate checkout and HOME removes a repository hook committed by another teammate for a target with a documented native hook aggregate projection
- **THEN** removal MUST refuse with `hook.remove-unowned`, identify the active native file and explain the ownership limitation
- **AND** configuration, acquisition locks, native hook files and ownership state MUST remain unchanged, with no successful-removal message

#### Scenario: Handwritten matches and unavailable proof cannot authorize removal

- **WHEN** a deselected hook matches an active handwritten native entry, or its ownership state is missing or corrupt
- **THEN** removal MUST refuse without adopting, deleting or rewriting the entry or publishing configuration or acquisition-lock changes
- **AND** diagnostics MUST NOT contain executable hook payloads

#### Scenario: Remaining contributors and owned removal stay supported

- **WHEN** a native entry remains selected by another hook, is absent, or is cleanly owned by the installation performing removal
- **THEN** removal MUST retain its ordinary selection and owned-entry cleanup behavior
- **AND** unrelated handwritten entries and entries in other scopes MUST remain unchanged

#### Scenario: File-based hook targets keep ordinary removal behavior

- **WHEN** OpenCode file-based hooks are selected or deselected without active native hook aggregates
- **THEN** the aggregate preflight MUST report no active aggregate residue and MUST NOT refuse the file-based hook removal
- **AND** its declaration and adapter files MUST remain unchanged by the preflight, with ordinary file-ownership and removal rules retained

# inspection-outcomes Specification

## Purpose
Makes read-only materialization health and diagnostic severity consistent for status, synchronization checks, and the terminal UI.

## Requirements

### Requirement: Inspection warnings remain non-fatal

The CLI and terminal UI SHALL report warning-severity materialization diagnostics without classifying the inspection as failed.

#### Scenario: Warning-only inspection

- **WHEN** an inspection produces warnings but no errors
- **THEN** it MUST report the warnings and retain a successful health result

### Requirement: Inspection errors fail checks

Synchronization checks SHALL report error-severity materialization diagnostics and return a nonzero status, including failures found during a dry-run.

#### Scenario: Materialization dry-run failure

- **WHEN** materialization fails during a sync check dry-run
- **THEN** sync check MUST report an error and exit unsuccessfully

### Requirement: Inspection consumers agree on issue severity

Status, synchronization checks, and the terminal UI SHALL classify malformed resources and materialization diagnostics from a shared inspection result. Warning-only issues MUST NOT fail the inspection; error-severity issues MUST fail. Reported source paths MUST NOT append a skill filename to a path that already names a file.

#### Scenario: Malformed resource warning

- **WHEN** a registry contains a malformed unselected resource with a path to its source file
- **THEN** status, sync check, and terminal UI MUST report it as a warning without failing, and show the file path once

#### Scenario: Materialization issue with or without a resource

- **WHEN** materialization reports a named, path-shaped, root-level, or operation-only issue
- **THEN** status, sync check, and terminal UI MUST preserve the issue's severity and source details, and an error-severity issue MUST cause failure

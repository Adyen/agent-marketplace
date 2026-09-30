## Purpose

Makes read-only materialization health and diagnostic severity consistent for status, synchronization checks, and the terminal UI.

## ADDED Requirements

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

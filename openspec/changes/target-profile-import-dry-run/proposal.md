# Proposal

## Why

Authors need to validate a local target-package import before curating or publishing it, without changing an installed setup. A deterministic dry-run makes mapping, containment, and package-boundary failures visible before any delivery workflow.

## What Changes

- Add the local `agent-marketplace target dry-run` command for a profile, source directory, import manifest, and optional destination.
- Validate the import as one target-scoped package and return a JSON report with selected mappings, explicitly reasoned source exclusions, advisory metadata, problem diagnostics, and optional destination conflicts and ownership transitions.
- Keep the command read-only and independent of organization configuration, approval, acquisition, installation, and host execution.

## Capabilities

### New Capabilities

- `target-profile-import`: Local target-package import validation and read-only dry-run reporting.

### Modified Capabilities

None.

## Impact

Adds a Cobra subcommand and local import/report handling in the CLI, reusing the existing target-profile validation, package normalization, and place-planning rules. Adds annotated CLI and parity test coverage; does not change target placement, Marketplace ingestion, TUI, web, or runtime verification behavior.

## Release note

Adds a local-only target import dry-run that reports package mappings, source exclusions, destination conflicts, and predicted ownership changes without installing files. It requires no organization configuration, approval, or running host.

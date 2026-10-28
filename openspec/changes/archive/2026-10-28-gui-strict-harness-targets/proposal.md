## Why

ADR 0010, accepted on `main`, makes an unknown Harness target ID in consumer configuration a configuration error. The desktop GUI branch was built on the opposite model: configuration readers kept unsupported top-level IDs with a warning, and the GUI let users edit and save targets while preserving them. Both rules ended up in the specifications and contradict each other.

## What Changes

- Remove the config-layering requirement that retains unsupported top-level Harness target IDs with a warning; configuration readers reject them as `main` does.
- The GUI shows a configuration error and refuses Harness target editing and synchronization while effective configuration names an unsupported target, and recovers after the user repairs the file.
- Replace the GUI scenario that preserved unknown identifiers on save with a supported-target save scenario.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `config-layering`: drop the retain-and-warn requirement for unsupported top-level Harness targets.
- `gui-configuration`: replace the Harness target form requirement with one that saves only supported targets and refuses configuration naming unsupported targets.

## Impact

`internal/config`, `internal/pipeline`, `internal/telemetry`, the GUI local API configuration path and its tests. CLI and TUI behavior already follow ADR 0010 and do not change.

## Release note

The desktop GUI refuses configuration that names an unsupported harness target, matching the CLI and TUI.

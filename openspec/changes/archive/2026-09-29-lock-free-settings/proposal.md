## Why

Reading logging and telemetry preferences should not fail or fetch unrelated remote acquisition evidence.

## What Changes

- Read logging and telemetry policy from validated authored configuration before remote acquisition.
- Keep effective configuration precedence across organization, user, repository, and personal-local layers.

## Capabilities

### New Capabilities

- `configuration-settings`: Settings read independently of remote acquisition locks.

### Modified Capabilities

None.

## Impact

CLI startup, logging, telemetry routing, and configuration validation.

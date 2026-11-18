## Why

When `status` stops on an acquisition lock or Marketplace error, its TARGET HARNESS SET section shows `none configured` with `-> add harnesses to ~/.agents/config.yaml`, even when the configuration declares harnesses. Reproduced on 2026-11-18 with a repository that selects Marketplace resources and has no acquisition lock: the user configuration declared `harnesses: [factory]`, yet `status` told the user to add harnesses. The message sends the user to fix the wrong file.

## What Changes

- When the configuration loads and declares harnesses, `status` shows the effective harness set and its source even if acquisition-lock or Marketplace inspection fails, and does not suggest adding harnesses.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `status-reporting`: status keeps the configured harness set when inspection fails.

## Impact

`cmd/agent-marketplace/status.go` and its tests. No configuration, lock, or API change.

## Release note

`status` now shows which harnesses are configured, and where they were declared, when acquisition-lock or Marketplace inspection fails. The original error and exit status are unchanged.

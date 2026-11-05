## Why

`main` removed the `agent-marketplace convert` subcommand (`remove-convert-subcommand`); harness translation is available only through the `pkg/harness-compilation` library. Two OpenCode requirements written before that removal still promise conversion help, `convert --from opencode`, `convert --to opencode`, and direct-conversion publication.

## What Changes

- OpenCode is accepted as both source and target of library conversion (`Convert`); the CLI conversion help and flags are no longer part of the requirement.
- Library conversion emits the OpenCode MCP configuration artifact with private permissions, and `Materialize` leaves that externally reconciled file to synchronization. The requirement no longer describes a direct-conversion publisher.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `harness-opencode`: conversion means library conversion.
- `opencode-native-configuration`: private modes cover synchronization and library conversion output.

## Impact

Specifications and tests only; the deleted command's OpenCode tests are replaced by a library conversion test.

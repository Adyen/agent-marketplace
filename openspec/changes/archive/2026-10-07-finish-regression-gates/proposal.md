## Why

The nightly real-terminal stress job stops at its 30-minute limit before completing all 20 iterations. The released-client gate still pins v4.0.8 and accepts its refusal of candidate-written locks, despite a newer stable release being available. These gaps prevent the regression-gate rollout from being complete.

## What Changes

- Give the unchanged 20-iteration nightly journey suite a measured job budget with setup and runtime headroom.
- Exercise bulk, tag, and ad-hoc install/remove after migration, including preservation and subsequent synchronization.
- Pin and checksum-verify v4.1.1, and require the released client to consume committed state after candidate sync, install, update, and remove.
- Require a healthy committed lock to remain byte-identical through plain candidate sync and released-client locked/check reads.
- Add `E2E Gates` to the existing default-branch ruleset without changing other protections.

## Capabilities

### New Capabilities

- `regression-gates`: released-client interoperability and complete nightly stress execution.

### Modified Capabilities

None.

## Impact

Changes are limited to CI configuration, gate tests, and regression-gate documentation. Production configuration and lock writers are unchanged. Repository protection is an explicitly approved external settings change.

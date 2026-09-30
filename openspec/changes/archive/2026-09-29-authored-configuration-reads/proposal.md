## Why

Ad-hoc browsing needs an authored registry allowlist, and prune-stale needs authored selection labels. Binding unrelated Marketplace sources for those reads could make both commands unavailable when acquisition evidence is missing.

## What Changes

- Apply the highest-precedence authored allowlist to ad-hoc browsing even if that layer cannot bind remote resources.
- Read informational selection labels from authored configuration without requiring remote acquisition.
- Keep bound resolution requirements for operations that need exact resources.

## Capabilities

### New Capabilities

- `authored-configuration-reads`: Allowlist enforcement and informational selection labels without unrelated acquisition.

### Modified Capabilities

None.

## Impact

`list --from`, prune-stale output, configuration intent, and remote acquisition boundaries.

## Why

Within one install scope, collection expansion skips a member whenever a direct selection already holds the same kind and installed name, without comparing required policy. When a required source selects a collection and a different, non-required source selects the same resource directly in the same install scope, only the non-required resource materializes, the required member never reaches the harness target, and no name-collision warning is reported. The cross-scope fix for collection members intentionally did not cover this same-scope case (#424).

## What Changes

- A required selection outranks a non-required selection of the same kind and installed name within one install scope, whether either side is selected directly or through a collection.
- Among equally required selections, a direct selection still beats a collection member, and the first expanded member still wins.
- A required collection member that displaces non-required selections reports one name-collision warning with the member as winner and each displaced source location as a runner-up. A displaced selection from the member's own source location produces no runner-up and no warning.
- Hooks keep their additive behavior.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-selection`: same-scope precedence now lets a required selection outrank a non-required one, including required collection members, and reports the displacement.

## Impact

Collection expansion in the resolver and its regression tests change: resolver unit tests, one command test that syncs in both orders, and one declarative E2E case. Configuration, lockfile, and diagnostic formats do not change.

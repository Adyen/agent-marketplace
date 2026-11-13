## Why

A lock operation rebuilds a source revision and catalog after exact Git snapshot validation, even when acquisition already built them. Reusing that result avoids repeated work without weakening fresh source verification or changing command behavior.

## What Changes

- Reuse a source revision and catalog already built in the same lock operation, only after existing exact Git snapshot validation succeeds.
- Preserve fresh source verification, lock and revision bytes, and independent-command behavior.
- Do not add payload byte memoization or reuse results across commands.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This internal optimization sets `skip_specs: true`; it changes no specified behavior.

## Impact

- Internal `sourcerevision` and `catalog` lock-operation paths.
- No API, dependency, command-output, or lock-format changes.

## Release note

none — Internal reuse preserves command output and lock formats.

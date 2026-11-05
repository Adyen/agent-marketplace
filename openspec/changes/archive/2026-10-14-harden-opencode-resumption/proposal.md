## Why

OpenCode full harness support must preserve security restrictions across the current consumer paths and pass its native compatibility gate. Resuming the integration exposed review findings around shared hook adapters, runner limits, model mappings, and native inspection.

## What Changes

- Preserve every accepted hook in multi-resource conversion and reject unsupported aggregate invocation counts before publication.
- Distinguish successful handlers that do not consume stdin from actual execution failures.
- Preserve explicit tool and model mapping decisions without emitting known-invalid model selections.
- Apply current source-read and publication safety boundaries to OpenCode resources.
- Repair native compatibility fixtures and inspection without weakening negative controls or running registry code.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `opencode-hooks`: reliable additive compilation, shared sync/conversion admission budgets, and explicit required-hook activation refusal.
- `harness-opencode`: faithful model/tool translation and current consumer safety boundaries.
- `opencode-native-configuration`: bounded captured identity inventory, supported aliases, and fail-closed potentially shadowed definitions.

## Impact

OpenCode compiler, ingester, materialization, and isolated compatibility tests. Existing harness defaults and acquisition-lock formats remain unchanged.

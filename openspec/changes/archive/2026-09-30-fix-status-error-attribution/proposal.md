## Why

Issue #418 reports that `status` marks independent same-named resources as not materialized when only one failed.

## What Changes

- Attribute failed materialization to the resource kind, installed name, install scope, and harness target that failed.
- Retain error reporting when a failure has no precise resource identity, without assigning an ambiguous failure to unrelated rows.

## Capabilities

### New Capabilities

- `status-reporting`: describe how status attributes resource materialization errors.

### Modified Capabilities

None.

## Impact

The materialization report, status CLI output, and their regression tests change. The shared diagnostic format does not change.

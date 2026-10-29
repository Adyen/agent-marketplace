## Why

Strict harness target validation is recorded only in the TUI interactions
specification even though the CLI shares the same consumer configuration
contract. Harness editing also needs a real-terminal refusal witness.

## What Changes

- Record the existing strict harness target ID contract in consumer-config-format.
- Extend the terminal journey to prove harness editing refuses invalid
  configuration and becomes available again after explicit repair.
- Use supported target IDs in the harness dialog resize benchmark.

Runtime behavior and validation remain unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `consumer-config-format`: require supported harness target IDs and bind
  CLI/TUI refusal, preservation and recovery to the terminal journey.

## Impact

Specification, decision-record, benchmark fixture and terminal assertions only.
No production code, dependency, configuration format or recovery changes.

## Release note

none - Documents and tests existing strict harness target validation without
changing runtime behavior.

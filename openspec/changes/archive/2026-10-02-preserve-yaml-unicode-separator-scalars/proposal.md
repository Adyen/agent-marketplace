## Why

The shared frontmatter formatter scans bytes for ASCII line breaks but misses YAML's Unicode line separators. A string description can therefore become separate native workflow metadata instead of remaining a scalar.

## What Changes

- Classify U+0085, U+2028 and U+2029 as scalar characters requiring quoted escaped output.
- Preserve those characters exactly through a strict native YAML parse without allowing description text to create workflow execution fields.
- Retain ordinary scalar formatting and legitimate typed native sidecars.

## Capabilities

### Modified Capabilities

- `harness-compilation`: Unicode separator scalar fidelity and native authority separation.

## Impact

Shared YAML frontmatter formatting under confirmed F99 / #433. No executed commands, native lifecycle policy, model/tool mapping or global processing-budget changes.

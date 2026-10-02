## Why

Hook support collection reads source bytes and mode through separate paths and silently drops read errors. A foreign source alias can import host payload, or a refused/unavailable support file can leave an updated native hook referring to content it did not install.

## What Changes

- Enumerate the authorized hook support directory through captured handles and reuse shared scoped regular snapshots before payload reads.
- Carry source refusal through work preparation, blocking affected hook output with a stable value-free diagnostic while retaining installed native content.
- Preserve contained aliases, executable permissions, target/source exclusions, collision semantics and optional absent source context.

## Capabilities

### Modified Capabilities

- `materialization`: scoped hook support-source admission and native refusal preservation.

## Impact

Next bounded R2 source-reader problem under #215/#431, dependent on #471. No new absolute byte/tree budget. Primary/hash/portable/dry-run readers and later output/ownership/pruning boundaries remain open.

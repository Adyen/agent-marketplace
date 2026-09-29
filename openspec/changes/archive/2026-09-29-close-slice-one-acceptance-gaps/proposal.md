## Why

The configuration-intent and cache-collection stack still retains provisional identity and duplicate loading paths. Command-level evidence for cold-cache collection and exact retention under a trusted organization acquisition lock is also missing.

## What Changes

- Reject duplicate Marketplace aliases when their instance IDs differ, including the former in-memory `bootstrap` placeholder.
- Remove the unused configuration-loading facade, duplicate deployed-lock loop, and nil-context fallbacks in cache operations while preserving caller behavior.
- Add offline command-level GC evidence for an empty cache and for exact archive retention alongside genuine orphan removal. Document these cases in the cache-collection specification.
- Update the as-built design and implementation plan and report before/after loader and GC benchmarks, including any slowdown.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cache-garbage-collection`: Make the empty-cache and trusted-organization-lock outcomes explicit as independently testable scenarios.
- `marketplace-protocols`: Specify that duplicate aliases with different Marketplace instance IDs are rejected, including the former provisional placeholder.

## Impact

Configuration validation and callers, cache context handling, GC command tests, the cache-garbage-collection spec, and developer-facing design, plan, and verification guidance. The existing acquisition lock format and TUI loading behavior remain unchanged.

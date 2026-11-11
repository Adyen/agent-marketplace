## Why

Every real sync deleted and recopied all support files in each cache-backed skill cache, even when nothing had changed (#976). On a registry shaped like the largest locally observed one (330 skills, 660 support files), warm `sync` took 3.7 s instead of 0.9 s without support files. It also gave every unchanged support file a new inode and modification time. The existing byte/mode no-op in support publication could never apply, because pruning first removed every destination.

## What Changes

- Before staging, skill cache preparation compares the cache's exact tree digest with the desired tree digest that `sync --check` already uses. When they match and the cache still opens below the admitted namespace, preparation returns without staging, pruning or writing.
- Any difference, missing or unreadable cache, source refusal or namespace change falls back to the existing full preparation, unchanged.

## Release note

A repeat `sync` no longer rewrites unchanged support files of cache-backed skills, so it is substantially faster for registries with many support files.

## Capabilities

### Modified Capabilities

- `materialization`: unchanged skill cache preparation becomes a no-op.

## Impact

Internal to `internal/materialize`. No new helper type, public API, dependency, cache, trust marker or limit. A first install with a missing cache fails the comparison before reading any source support. Changed caches pay for one digest comparison before the existing full preparation.

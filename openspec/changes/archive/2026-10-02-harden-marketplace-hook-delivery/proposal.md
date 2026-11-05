## Why

PR #470 review found untrusted index-only support declarations, unsafe shell path
substitution, silent missing-file installation, and cache-backed execution.
Stacking on native-hook-integration provides the compiler support-base API.

## What Changes

- Install mirrored support as owned regular files in the declared per-hook
  support directory, with no executable dependency on the cache.
- Verify the exact bytes consumed by materialization and refuse unavailable,
  unsafe, or native-definition support files.
- Quote literal support paths in the compiler, preserving safe legacy output.
- Anchor publication reads at acquisition roots and report unsupported references
  at publication with an achievable Git-source remedy at consumption.
- Preserve deterministic refusal codes during offline cached resolution.

## Capabilities

### Modified Capabilities
- `marketplace-api`: Harden mirrored hook delivery and installation.

## Impact

Marketplace ingest/resolver, hook support materialization, and hookpolicy command
rewriting. Does not implement the remaining native ownership/migration work in
native-hook-integration.

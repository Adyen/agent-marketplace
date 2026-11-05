## Why

F70 (2026-09-24 report), transferred from R4 to R2 (#431): Codex agents whose names differ only in letter case compile to `.codex/agents/Foo.toml` and `.codex/agents/foo.toml`. Exact-path conflict detection accepted both.

On a case-insensitive filesystem, reproduced on an APFS disk image, these name one file:
- `Materialize` failed with `file exists`.
- A real `sync` failed with `materialize.write.error … generation conflict` and wrote nothing for the Codex target. The report's misattribution claim does not reproduce, because staged publication fails closed.
- The residual is availability: one case-only pair blocks every output of the target, with an opaque error.

## What Changes

- Bundle artifact-path conflict detection compares paths case-insensitively.
- Installation refuses selected resources of one kind, target and scope whose names differ only in letter case, before any write. This covers the `sync` path, which compiles each resource separately, including collisions across sources.
- A case-only collision suppresses both producers with the existing conflict error and a message naming both paths. Every other resource still installs.
- The rule is host-independent, so case-sensitive systems refuse the same pair and shared repositories stay portable (maintainer decision on #431).

## Capabilities

### Modified Capabilities

- `harness-compilation`: artifact paths that differ only in letter case collide.

## Impact

`resolveArtifactPathConflicts` and the installation work loop. Resources whose names differ only in letter case are now refused on every host, where case-sensitive hosts previously installed both. MCP servers and hooks are unaffected: they are entries keyed by name inside shared files.

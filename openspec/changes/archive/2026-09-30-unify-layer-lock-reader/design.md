## Context

Single-file config loading and `internal/pipeline/acquisition.go` read deployed locks through separate paths. Pipeline callers depend on `LockViewError` codes and unsafe-path mapping; config callers record canonical and requested paths with a digest. Both choose canonical overlay data before requested overlay data.

## Goals / Non-Goals

**Goals:** Centralize bounded read, decode, scope validation, overlay precedence, digest, and v1 warning while retaining existing caller-specific error identities and read metadata.

**Non-Goals:** Change the acquisition lock format, organization trust checks, online freshness/missing-lock policy, or CLI interface.

## Decisions

- `dependencytxn.ReadLayerLock` accepts canonical and requested lock paths, scope, and overlays, returning `LayerLockEvidence` with the document, digest, and overlay flag plus a typed validation cause. It uses descriptor-bound reads for managed paths and strict reads for legacy paths, without adding an acquisition policy loop or making config depend on pipeline.
- Pipeline translates typed validation failure to `LockViewError{Code: "lock.invalid"}` and preserves unsafe-path mapping; config retains its loader-specific read/decode errors and `ConfigRead` metadata. An alternative shared outward error type would change existing callers' error identity.
- Config resolves paths through `ResolveConfigLockPathsTolerateMissing` before passing lock paths to the reader, preserving the logical layer lock location when config uses an approved link. Keep trust and missing-lock policy outside it because they differ by layer and mode.

## Risks / Trade-offs

- A wrong-scope lock previously accepted by config's single-file decoder now fails closed. Pin the intended failure and a valid control through a real single-file loader.
- Moving warning emission could duplicate v1 warnings if the reader is invoked twice. Test warning count on each path and keep it at one warning per read.

## Migration Plan

No stored data changes. Deploy the code change with its tests and OpenSpec update; rollback restores the previous reader behavior.

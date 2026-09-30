## Context

`internal/config/intent.go` reads adjacent locks for single-file loaders, while `internal/pipeline/acquisition.go` independently reads, decodes, and scope-validates deployed locks. Pipeline callers depend on `LockViewError` codes and unsafe-path mapping; config callers record canonical and requested paths with a digest. Both choose canonical overlay data before requested overlay data.

## Goals / Non-Goals

**Goals:** Centralize bounded read, decode, scope validation, overlay precedence, digest, and v1 warning while retaining existing caller-specific error identities and read metadata.

**Non-Goals:** Change the acquisition lock format, organization trust checks, online freshness/missing-lock policy, or CLI interface.

## Decisions

- A shared `internal/lockfile` reader accepts canonical and requested lock paths, scope, and overlays, returning the document, digest, existence, overlay flag, and a typed validation cause. This removes the two decoders without introducing another acquisition policy loop. An alternative shared pipeline helper would make config depend on pipeline and invert the existing dependency direction.
- Pipeline translates typed validation failure to `LockViewError{Code: "lock.invalid"}` and maps unsafe files to `dependencytxn.ErrUnsafePath`; config retains its loader-specific read/decode errors and `ConfigRead` metadata. An alternative shared outward error type would change existing callers' error identity.
- The caller resolves and validates configuration paths before passing lock paths to the reader. Keep trust and missing-lock policy outside it because they differ by layer and mode.

## Risks / Trade-offs

- A wrong-scope lock previously accepted by config's single-file decoder now fails closed. Pin the intended failure and a valid control through a real single-file loader.
- Moving warning emission could duplicate v1 warnings if the reader is invoked twice. Test warning count on each path and keep it at one warning per read.

## Migration Plan

No stored data changes. Deploy the code change with its tests and OpenSpec update; rollback restores the previous reader behavior.

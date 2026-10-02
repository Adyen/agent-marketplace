## Why

Catalog requests repeatedly decode managed configuration and decorate more candidate rows than a page needs. Matching conditional requests still hydrate a complete page, while existing request metrics exclude authority and availability work.

## What Changes

- Add bounded full-stack and phase metrics without redefining existing request series.
- Reuse one authoritative managed configuration read within each public request, preserving fresh state between requests and fail-closed behavior.
- Add a guarded conditional catalog read that avoids full response construction without suppressing existing corruption checks.
- Select eligible winning page keys before wide decoration in PostgreSQL and SQLite.
- Retain representative full-handler benchmarks and use private profiles to select allocation reductions.
- Preserve public wire formats, revocation and policy freshness, keyset ordering, and read-only requests.

## Capabilities

### New Capabilities

- `marketplace-catalog-performance`: Observable catalog safety, conditional-read compatibility, and bounded operations timing across both metadata backends.

### Modified Capabilities

None.

## Impact

Marketplace public request middleware, catalog handlers, both metadata stores, shared store interfaces, adjacent tests and benchmarks, and operations metric documentation. No schema migration, configuration knob, pool retuning, cross-request cache, client or frontend change, or deployment change.

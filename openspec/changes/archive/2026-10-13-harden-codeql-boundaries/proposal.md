## Why

GitHub CodeQL identifies unsafe blob addresses, incomplete inspection content scheme checks, integer conversion boundaries, unchecked allocation hints, unencoded log values, and unsafe test proxy/file operations. Correct the boundaries together without changing digest identities, ownership keys, acquisition locks, or valid materialization output.

## What Changes

- Contain filesystem blob operations within the configured storage root and reject unsafe addresses.
- Keep active URL scheme support content metadata-only at projection and publication boundaries.
- Reject integer metadata that cannot be represented without wrapping.
- Encode untrusted log attributes before handing records to handlers.
- Replace arithmetic allocation hints with bounded hints and use descriptor-based test reads and fixed-origin proxy URLs.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-api`: blob containment and active-scheme inspection policy.
- `harness-compilation`: representable integer metadata.
- `configuration-settings`: single-record encoding of untrusted operational log values.

## Impact

CLI and Marketplace logging, filesystem blob storage, native metadata ingestion, artifact/backup path guards, allocation hints, and JavaScript test infrastructure. Existing SHA-256 formats remain unchanged. Five verified hashing false positives are dismissed with evidence rather than altering compatible formats.

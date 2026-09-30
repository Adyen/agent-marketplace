## REMOVED Requirements

### Requirement: Local harness-compilation package

**Reason**: Describes repository module layout after a completed migration, not
user-observable behavior.
**Migration**: None. The layout is enforced by the Go toolchain and build.

### Requirement: Path-rewritten upstream history

**Reason**: A one-time Git history property of a completed migration; not
behavior and not testable from the product.
**Migration**: None.

### Requirement: Local import paths

**Reason**: Source-code organization, not user-observable behavior.
**Migration**: None. The Go build fails on any unresolved import.

### Requirement: Existing HCC-backed behavior remains compatible

**Reason**: A migration-time acceptance gate rather than a standing contract;
the behavior it protected is specified by the harness capabilities added in
later baseline changes.
**Migration**: See the `harness-ingestion`, `harness-compilation`, and
`diagnostics` capabilities once baselined.

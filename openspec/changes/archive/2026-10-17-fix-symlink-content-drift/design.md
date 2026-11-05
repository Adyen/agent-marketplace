## Decision

Reuse the existing SDK dry-run materializer to compare compiled cache content.
Build desired support-file artifacts with the existing captured source walker,
including its source-admission rules, and request stale-file checking only for
resource trees whose support content real sync prunes. Support-file permission
changes also require reconciliation.

Both grouped resource links and ordinary artifact links must apply the
comparison before declaring an unchanged managed symlink synchronized. Count
the native installed path once, not the internal cache paths. Preserve
user-owned collision classification before comparing a cache payload.

Changing cache identities to content hashes would alter the cache contract.
Running real cache preparation during a check would mutate installed content.
Neither is needed.

## Verification

Start with failing cross-target tests for skills, commands and subagents at
repository and machine scopes, plus support edits, additions, removals,
permissions and missing caches. Prove actual synchronization updates the
payload and restores a clean check without replacing a stable symlink.
Exercise the public CLI using a live directory source and fingerprint
configuration, native outputs and caches across checks. Benchmark healthy
checks with zero and sixteen support files, including disk I/O but no network.

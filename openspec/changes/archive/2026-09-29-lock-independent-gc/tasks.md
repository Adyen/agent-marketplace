## 1. Collection boundaries

- [x] 1.1 Collect without an organization acquisition lock while retaining live cache objects; verify `TestGcCommandV4OrganizationMarketplaceWithoutLockCollects` passes.
- [x] 1.2 Refuse deletion for invalid deployed evidence; verify `TestGcCommandV4OrganizationMarketplaceLockMismatchFailsClosed` passes.

## 2. Bind and archive

- [x] 2.1 Annotate both cache-garbage-collection scenario tests; verify `go run ./tools/specaudit trace` passes.
- [x] 2.2 Run strict OpenSpec validation and focused GC tests; verify both commands pass.
- [x] 2.3 Run `openspec archive lock-independent-gc --yes` as the final change in this pull request; verify the archived spec is present.

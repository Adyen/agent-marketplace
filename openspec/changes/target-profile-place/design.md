# Design

The normative design is `docs/plans/2026-11-13-target-profiles-design.md` on `feat/target-profiles`. It is the authority for the areas below, and this change implements it.

| Area | Design decisions |
| --- | --- |
| Profile identity, selection and assessment binding | 3, 7, 10, 13 |
| Approval and retirement | 7 |
| Host conditions | 8 |
| Verification boundaries | 6, 12 |
| Place publication, ownership and recovery | 14 |

Settled decisions this change adds to or confirms in the design:

- **Decision 14 coverage.** The "remaining edges" paragraph lists required witnesses, not open decisions: created-directory identity, prune generations, verification retention, journal authority and order, nearest-ancestor device checks, deleted files, retry after edits, root spelling and symlinks, absent stale files, and generation references. Each has a scenario here and an annotated test.
- **Deleted owned files.** A user-deleted file of an installed package, found during an ordinary sync with no crash involved, is treated as an edit. The rest of the projection is kept and the file is not recreated. The deletion is reported as drift. An upgrade refuses the new revision as a unit, and deselection cedes the path.
- **Test-only profiles.** Only test profiles are used. Tern and any unverified host or proxy evidence are excluded. Real host profiles start with the Herdr app target change.
- **Prerequisite stack.** Resource requirements (`requires`) are already on the integration branch, and place packages keep the existing prerequisite semantics.

Implementation seams named by Decision 14:

- `dependencytxn.ClassifyDestination`, `commitLocked`, `publish`, `validateStagingPath`, `validateJournal`, `CheckPending`, `ReadPendingPluginGenerationRefs` and `fault_e2e.go`.
- `packagegen.BuildKey`, `PublishUnderPackagesLock` and `GarbageCollect`.
- `ownership.Store.PrepareBatch`, `CanonicalRoot`, `RootKey` and `ReadAllPluginGenerationRefs`.
- `fsboundary.OpenDir`, `CaptureDestination`, `Destination.Verify`, `Publish`, `RemoveLeaf` and `Dir.Rmdir`.
- `materialize/destination_boundary.go:captureDirectory`.

Place writes never use `AuthorityLegacy`, the pre-journal `os.MkdirAll` loop in `plugin_batch.go`, or the per-root ownership journal.

Observation limits remain limits and are not promised as guarantees:

- concurrent moves of a root ancestor between the re-stat and the write;
- races between leaf verification and `renameat` or `unlinkat`;
- same-device bind mounts;
- inode reuse;
- whether a host reloads during publication.

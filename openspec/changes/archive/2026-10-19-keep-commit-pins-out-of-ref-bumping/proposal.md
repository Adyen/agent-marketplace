## Why

P19 in the #215 frozen ledger, finding F149 of the 2026-09-24 report (#758). `update --bump` treats every ref that ends in a number as a version. A full commit ID such as `…4959df631` matches that pattern, so a tag named after the commit ID's non-numeric prefix with a larger number becomes the bump target. The configured ref is rewritten to the tag, the acquisition lock advances, and the tag's content is synced.

A user pins an exact commit to stop taking new content from a registry. Through this path the registry owner can replace that pin the next time the user runs `update --bump`. Reproduced on `main` at `f44101fa`: `update --bump` reports `refs bumped: 1`, rewrites the commit ref to the lookalike tag, and installs the replacement skill.

## What Changes

- A source ref that is a full Git object ID, 40 hexadecimal characters or 64 in a SHA-256 repository, in either letter case, is an exact commit pin. `update --bump` leaves it unchanged, does not count it as bumped, and does not fetch for it.
- Other refs keep today's bump behavior, including version refs in the same registry.
- Abbreviated object IDs remain ordinary refs. A tag with the same name already takes precedence over them in Git, so they are not exact pins today.

## Capabilities

### Modified Capabilities

- `resource-selection`: ref bumping leaves exact commit pins unchanged.

## Impact

`bumpRepoRegistryRefs` in `internal/pipeline/update.go` skips full object IDs before the mutable-cache fetch, using a new `giturl.IsFullObjectID` predicate. Configuration, lockfile and output formats do not change. Users who pinned a full commit ID that ends in digits no longer see it rewritten by `update --bump`.

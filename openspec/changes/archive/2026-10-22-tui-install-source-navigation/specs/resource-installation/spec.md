## ADDED Requirements

### Requirement: Source previews reuse a current cache

A preview of an unconfigured Git source without a ref MUST look up the
remote's default branch and the commit it advertises in one remote request.
When the cached checkout of that branch is already at the advertised commit
and contains the requested path, the preview MUST NOT fetch and MUST report
the advertised commit; otherwise it MUST refresh the cache as before. A
preview whose ref is a full commit SHA MUST NOT access the network when the
cached checkout of that ref is already at that commit and contains the
requested path. Reusing the cache MUST NOT change the preview's resources,
warnings, or reported commit compared with a refreshed preview of the same
commit.

#### Scenario: Default-branch preview skips the fetch when the cache is current

- **WHEN** a source is previewed without a ref and the cached checkout of its
  default branch is at the commit the remote advertises
- **THEN** the preview MUST NOT fetch and MUST report that commit

#### Scenario: Default-branch preview refreshes a stale cache

- **WHEN** a source is previewed without a ref after its default branch
  advanced past the cached commit
- **THEN** the preview MUST fetch and MUST report the advanced commit and its
  resources

#### Scenario: Commit-pinned preview works from the cache offline

- **WHEN** a source is previewed with a full commit SHA as the ref, the cached
  checkout is at that commit, and the remote is unreachable
- **THEN** the preview MUST succeed with that commit and its resources

## Purpose

Define when symlinks in a Git registry repository affect exact snapshot acquisition and how the configured source root constrains source revision validation.

## ADDED Requirements

### Requirement: Exact snapshot acquisition is independent of unselected symlinks

An exact snapshot SHALL remain acquirable and reusable when its verified Git tree contains dangling or escaping symlinks outside the configured source root. Exact tracked-tree integrity checks MUST still reject mismatched content.

#### Scenario: Snapshot contains unselected unsafe symlinks

- **WHEN** a registry repository's exact commit contains dangling and escaping symlinks outside the configured source root
- **THEN** acquisition and reuse of the exact snapshot MUST succeed when its tracked tree matches the commit

#### Scenario: Snapshot tracked tree is altered

- **WHEN** a cached exact snapshot is opened read-only after its tracked contents differ from its commit
- **THEN** the open MUST reject the snapshot as untrusted, irrespective of symlink validation scope

### Requirement: Configured source root bounds symlink validation

Source revision creation SHALL accept symlinks outside the configured source root without validating their destinations. It MUST reject unresolved symlinks within that root and symlinks within it that resolve outside that root, without incorporating those targets into the source revision.

#### Scenario: Narrowed source root excludes unsafe symlink

- **WHEN** a configured source root excludes a dangling symlink elsewhere in the same registry repository
- **THEN** source revision creation for that root MUST succeed while creation for a root containing the link MUST fail

#### Scenario: Selected unsafe symlink identifies remediation

- **WHEN** a symlink inside the configured source root is dangling or resolves outside that root
- **THEN** source revision creation MUST fail with a path escape error that identifies the registry-relative link path, its authored target, the configured root, and remediation to remove the link or narrow the configured path

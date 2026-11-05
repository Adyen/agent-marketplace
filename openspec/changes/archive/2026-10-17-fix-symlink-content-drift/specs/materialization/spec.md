## ADDED Requirements

### Requirement: Read-only checks compare cache-backed resource content

In symlink materialization mode, Agent Marketplace MUST compare desired
resource content with the cache content exposed by an installed managed
symlink before reporting it synchronized. Changed or missing compiled content,
added, changed or removed support files, and support-file permission changes
MUST report the affected native resource path as a pending write even when
the symlink target is unchanged. Read-only checks MUST NOT populate, repair
or prune resource caches or replace installed links. Unchanged content MUST
remain synchronized, user-owned collisions MUST retain their existing
preservation policy, and real synchronization MUST restore a clean check.

#### Scenario: Check detects changed linked resource content

- **WHEN** compiled content of an installed linked resource changes while its desired cache path remains stable
- **THEN** read-only materialization MUST report the native resource path as a pending write without changing installed cache bytes, permissions, modification time or symlink identity
- **AND** real synchronization MUST update the exposed content and a subsequent check MUST report it synchronized

#### Scenario: Check detects linked support file drift

- **WHEN** an installed linked resource has added, edited or removed support files, changed support-file permissions, or missing cache content
- **THEN** checking MUST report the resource as pending without updating existing cache content or recreating missing cache content
- **AND** real synchronization MUST reconcile the support content and restore a clean check

#### Scenario: CLI check refuses stale linked content without repairing it

- **WHEN** a directory source changes the content of an already synchronized linked resource
- **THEN** `sync --check` MUST exit 1 and preserve configuration, acquisition locks, installed links and resource caches
- **AND** ordinary sync MUST expose the new content and a subsequent `sync --check` MUST exit 0 without changes

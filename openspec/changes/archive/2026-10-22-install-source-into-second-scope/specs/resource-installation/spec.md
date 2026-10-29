## ADDED Requirements

### Requirement: Explicit-source installs bind to the target scope's source

An explicit-source install MUST resolve the source's resources from the
target scope's declaration of that source and that scope's acquisition lock,
even when another scope already declares the same URL, ref, and path and its
lock pins a different commit. The install MUST NOT change the other scope's
configuration or acquisition lock.

#### Scenario: Source locked in another scope installs into the target scope

- **WHEN** the project acquisition lock pins a Git source's ref at an older
  commit and an explicit-source install selects a resource from that source
  into local scope
- **THEN** the install MUST succeed, local configuration MUST record the
  source and the resource, the local acquisition lock MUST record the commit
  the ref currently resolves to, project configuration and acquisition lock
  bytes MUST be unchanged, and `sync --check` MUST pass after a sync

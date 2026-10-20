## ADDED Requirements

### Requirement: Writing sync records selected Marketplace resources missing from an existing lock

A writing `lock` or `sync` for a selected user, repository, or personal-local layer SHALL add an acquisition lock record for every Marketplace resource the layer selects that its existing lock does not record, resolving it from the endpoint the lock already identifies. Records the lock already has MUST stay exact, including their pinned revisions, unless the configuration requests a different explicit revision for that resource. Other Marketplace and Git records MUST remain exact. Read-only dependency modes and `doctor` MUST NOT fetch or write: they MUST keep reporting `v4.marketplace-lock-mismatch` for the missing record. Organization locks MUST NOT be written implicitly.

#### Scenario: Writing sync adds a missing selected resource and keeps existing pins

- **WHEN** a repository configuration selects two resources from a Marketplace endpoint and the existing repository acquisition lock records the endpoint and only the first resource, and a writing `agent-marketplace sync` runs while online
- **THEN** the lock MUST gain a record for the second resource
- **AND** the first resource's record MUST keep its pinned revision
- **AND** `doctor` MUST no longer report `v4.marketplace-lock-mismatch`

#### Scenario: Read-only modes leave a missing resource record unwritten

- **WHEN** the same configuration and lock are checked with `agent-marketplace sync --check`, `sync --locked`, `sync --offline`, or `doctor`
- **THEN** the lock bytes MUST stay unchanged and the command MUST report `v4.marketplace-lock-mismatch` for the missing resource

## ADDED Requirements

### Requirement: Lock creation skips Marketplace selections removed upstream

When a writing `lock` or `sync` adds an acquisition lock record for a
non-required Marketplace selection and the selection's latest-revision lookup
returns HTTP 404, it SHALL record no entry for that selection, report a warning
with the remedy, and record and materialize every other selection. For a
required selection it SHALL instead fail without writing the lock. Other
Marketplace failures SHALL keep failing the run without writing the lock.

#### Scenario: Optional removed selection is skipped while the rest is locked

- **WHEN** a repository configuration selects two Marketplace resources, no repository acquisition lock exists, one resource's latest-revision lookup returns 404, and a writing `agent-marketplace sync` runs online
- **THEN** the lock MUST be written with a record for the available resource only, and that resource MUST be installed
- **AND** a warning MUST name the removed resource's Marketplace URL, registry, kind, and name with the `agent-marketplace remove <installed name>` remedy
- **AND** `sync` MUST exit 0

#### Scenario: Required removed selection fails without writing the lock

- **WHEN** the removed selection is required
- **THEN** `sync` MUST exit 1 with an error naming the resource and the configuration entry that marks it required
- **AND** no repository acquisition lock MUST be written

#### Scenario: Other lookup failures still fail without writing the lock

- **WHEN** the latest-revision lookup fails with a server error rather than 404
- **THEN** `sync` MUST exit 1 and no repository acquisition lock MUST be written

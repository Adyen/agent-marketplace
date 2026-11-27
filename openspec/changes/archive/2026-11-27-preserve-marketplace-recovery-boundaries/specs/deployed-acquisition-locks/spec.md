# Spec Delta

## MODIFIED Requirements

### Requirement: Lock creation skips only unrecorded optional direct Marketplace selections removed upstream

A writing `lock` or `sync` MUST skip only an unrecorded optional direct
Marketplace selection without an explicit revision when the latest-revision
lookup reports that the resource itself is missing (HTTP 404). Skipping a
selection MUST NOT discard an existing pin. Other selected resources follow
normal lock rules; an explicit revision requested by configuration may replace
its pin. A missing required selection, unavailable explicit revision, missing
registry, endpoint-level 404, or other acquisition failure MUST fail without
writing a new lock. `lock` records the remaining selections but does not install
them. Plain `sync` records and installs the remaining selections and exits 0
when no other work fails. `sync --strict` performs the same synchronization,
reports the skip warning as an error, and exits 1.

Skipping an effective selection MUST NOT restore a superseded selection of the same locator from the same source.

#### Scenario: Optional removed selection is skipped while the rest is locked

- **WHEN** a repository configuration selects two unrecorded optional direct Marketplace resources, one resource's latest-revision lookup reports that the resource is missing with HTTP 404, and a writing `lock` or `sync` runs online
- **THEN** the lock MUST contain only the available resource, and `lock` MUST NOT install it
- **AND** `sync` MUST install the available resource and MUST NOT install the removed resource
- **AND** a warning MUST name the removed resource's Marketplace URL, registry, kind, and name with the `agent-marketplace remove <installed name>` remedy
- **AND** plain `sync` MUST exit 0

#### Scenario: Required removed selection fails without writing the lock

- **WHEN** the missing selection is required
- **THEN** `sync` MUST exit 1 with an error naming the resource and the configuration entry that marks it required
- **AND** no repository acquisition lock MUST be written

#### Scenario: Strict sync reports optional removed selection after installing remaining

- **WHEN** a repository configuration selects an unrecorded optional direct Marketplace resource and an available resource, the missing resource's latest-revision lookup reports resource-not-found with HTTP 404, and `sync --strict` runs online
- **THEN** the lock MUST contain only the available resource
- **AND** the available resource MUST be installed and the removed resource MUST NOT be installed
- **AND** the command MUST report the removed resource and its `agent-marketplace remove <installed name>` remedy
- **AND** the command MUST exit 1 after completing the remaining synchronization

#### Scenario: Other lookup failures still fail without writing the lock

- **WHEN** a Marketplace lookup fails because a registry is missing, an explicit revision is unavailable, or the server returns an error
- **THEN** `sync` MUST exit 1 and no repository acquisition lock MUST be written

#### Scenario: Skipping a root selection does not restore a fragment pin

- **GIVEN** an included fragment selects a Marketplace revision and the root overrides it with an unrecorded optional direct selection of the same locator without an explicit revision
- **WHEN** plain `sync` receives a resource-level 404 for that selection's latest revision
- **THEN** it MUST NOT request the superseded revision or install either form of the skipped selection
- **AND** it MUST report the removal remedy and lock and install the available sibling

## ADDED Requirements

### Requirement: Marketplace selections are removable without a lock record

Named removal SHALL accept a Marketplace selection from a writable user,
repository, or personal-local layer when that layer has no acquisition lock or
its lock has no record for the selection. Removal SHALL edit only the
configuration, SHALL make no Marketplace request for the removed selection, and
SHALL then synchronize as usual. Unsupported or invalid configuration roots and
required selections SHALL keep their existing refusals.

#### Scenario: Remove a Marketplace selection when the layer has no lock

- **GIVEN** a repository configuration selects two Marketplace resources, no repository acquisition lock exists, and one resource's latest-revision lookup returns 404
- **WHEN** the user runs `agent-marketplace remove <name> --scope repo` for that resource
- **THEN** removal MUST succeed and remove only that selection from the configuration
- **AND** the following `sync` MUST exit 0, write a lock recording the remaining resource, and install it

#### Scenario: Remove a Marketplace selection missing from an existing lock

- **GIVEN** a repository acquisition lock records the endpoint but not a selected resource
- **WHEN** the user removes that resource by name
- **THEN** removal MUST succeed and leave the lock's other records unchanged

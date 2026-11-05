## ADDED Requirements

### Requirement: A missing Marketplace resource does not block other pin updates

When the Marketplace answers the latest-revision lookup for a user-managed Marketplace pin with HTTP 404, the `update` command SHALL treat that resource as missing rather than aborting. It SHALL keep the missing pin at its current pinned revision with its lock entry unchanged, plan, update, lock, and sync every other pin as it would otherwise, and write its normal update report. It SHALL then exit with status 1 and an error that names every missing resource by Marketplace URL, registry, kind, and name, and that tells the user to remove the old pin with `agent-marketplace remove <installed name>` and install the resource under its new name. This applies to resource pins and collection pins alike.

#### Scenario: Missing resource is kept while other pins update

- **WHEN** `update` runs with one Marketplace pin whose latest-revision lookup returns 404 and another pin that has a newer revision
- **THEN** it SHALL advance the other pin in the lock and harness outputs and write the update report
- **AND** it SHALL leave the missing pin's lock entry byte-identical
- **AND** it SHALL exit with status 1 and an error naming the missing resource's Marketplace URL, registry, kind, and name with the remove-and-install hint

#### Scenario: Lock and outputs agree after a partial update

- **WHEN** `update` has kept a missing Marketplace resource and updated other pins
- **THEN** a following `sync --check` SHALL report no drift

#### Scenario: Only missing resources change nothing

- **WHEN** every Marketplace pin that `update` checks returns 404 for its latest-revision lookup and no Git registry is configured
- **THEN** `update` SHALL not stage or sync anything and SHALL leave configuration, lock, and harness outputs byte-identical
- **AND** it SHALL name each missing resource and exit with status 1

#### Scenario: Every missing resource is named

- **WHEN** more than one Marketplace pin returns 404 for its latest-revision lookup during one `update`
- **THEN** the error SHALL name every missing resource, not only the first one checked

#### Scenario: Missing collection is kept

- **WHEN** a Marketplace collection pin's latest-revision lookup returns 404 during `update`
- **THEN** `update` SHALL keep that collection pin and its members unchanged, continue with the other pins, name the collection in the error, and exit with status 1

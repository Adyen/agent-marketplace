## MODIFIED Requirements

### Requirement: A missing Marketplace resource does not block other pin updates

When the Marketplace answers the latest-revision lookup for a user-managed
Marketplace pin with HTTP 404, an `update` invocation SHALL treat that resource
as removed upstream rather than aborting. It SHALL keep the removed pin at its
current pinned revision with its lock entry unchanged, and plan, update, lock,
and sync every other pin as it would otherwise. This applies to resource pins
and collection pins alike.

A removed pin's pinned revision is usable when the Marketplace still serves that
exact revision and it passes the existing assessment and revocation checks, or
when the verified local cache holds its revision metadata and artifact. For a
removed pin with a usable pinned revision, `update` SHALL report a warning that
names the resource by Marketplace URL, registry, kind, and name, states that the
pinned revision is kept, and tells the user to drop it with
`agent-marketplace remove <installed name>`. For a required pin, the warning
SHALL instead name the configuration entry that marks it required and SHALL NOT
suggest `agent-marketplace remove`. Such a warning SHALL NOT by itself make
`update` exit nonzero, and it SHALL be reported again on every `update` while
the pin remains selected.

For a removed pin whose pinned revision is not usable, `update` SHALL write its
normal report and then exit with status 1 and an error that names every such
resource by Marketplace URL, registry, kind, and name, and that tells the user
to remove the old pin with `agent-marketplace remove <installed name>` and
install the resource under its new name.

A preview SHALL keep all persistent state unchanged and report the same
warnings and errors with the same exit status. A removed pin whose current
revision is retained SHALL have no proposed content change; its absent latest
candidate SHALL NOT be treated as a diff-generation failure for other eligible
updates.

#### Scenario: Missing resource is kept while other pins update

- **WHEN** `update` runs with one Marketplace pin whose latest-revision lookup returns 404 while its exact pinned revision is still served, and another pin that has a newer revision
- **THEN** it SHALL advance the other pin in the lock and harness outputs and write the update report
- **AND** it SHALL leave the removed pin's lock entry byte-identical and its installed content unchanged
- **AND** it SHALL report a warning naming the removed resource's Marketplace URL, registry, kind, and name with the `agent-marketplace remove <installed name>` hint, and exit 0

#### Scenario: The warning repeats on every update

- **WHEN** `update` has kept a removed resource with a warning and `update` runs again without configuration changes
- **THEN** it SHALL report the same warning again and exit 0

#### Scenario: Cached pinned revision is usable

- **WHEN** a pin's latest-revision lookup and exact revision lookup both return 404 and the verified local cache holds the pinned revision's metadata and artifact
- **THEN** `update` SHALL keep the pin with the warning and exit 0

#### Scenario: Unusable pinned revision remains an error

- **WHEN** a pin's latest-revision lookup and exact revision lookup both return 404, the verified local cache does not hold the pinned revision, and another pin has a newer revision
- **THEN** `update` SHALL advance the other pin, keep the removed pin's lock entry byte-identical, and exit with status 1 and an error naming the removed resource's Marketplace URL, registry, kind, and name with the remove-and-install hint

#### Scenario: Required removed pin names its required entry

- **WHEN** a required Marketplace pin's latest-revision lookup returns 404 while its exact pinned revision is still served
- **THEN** `update` SHALL keep the pin, report a warning that names the configuration entry marking it required without suggesting `agent-marketplace remove`, and exit 0

#### Scenario: Lock and outputs agree after a partial update

- **WHEN** `update` has kept a removed Marketplace resource and updated other pins
- **THEN** a following `sync --check` SHALL report no drift

#### Scenario: Only missing resources change nothing

- **WHEN** every Marketplace pin that `update` checks returns 404 for its latest-revision lookup, every pinned revision is usable, and no Git registry is configured
- **THEN** `update` SHALL not stage or sync anything and SHALL leave configuration, lock, and harness outputs byte-identical
- **AND** it SHALL report a warning for each removed resource and exit 0

#### Scenario: Every missing resource is named

- **WHEN** more than one Marketplace pin returns 404 for its latest-revision lookup during one `update`
- **THEN** the warnings and errors SHALL name every removed resource, not only the first one checked

#### Scenario: Missing collection is kept

- **WHEN** a Marketplace collection pin's latest-revision lookup returns 404 during `update` and its pinned revision is usable
- **THEN** `update` SHALL keep that collection pin and its members unchanged, continue with the other pins, name the collection in a warning, and exit 0

#### Scenario: Missing resource preview retains state and reports other candidates

- **GIVEN** one Marketplace pin whose latest-revision lookup returns 404 with a usable pinned revision and another with a newer revision
- **WHEN** `update --dry-run --include-diff` runs
- **THEN** it SHALL keep both pins and installed content unchanged, name the removed resource in a warning with the remove hint, show only the other pin's proposed diff, and exit 0

#### Scenario: Applied diffs retain partial update behavior

- **GIVEN** one removed Marketplace resource with a usable pinned revision and another eligible resource with changed content
- **WHEN** `update --include-diff` runs
- **THEN** it SHALL update the eligible resource and report its applied diff, identify the kept removed resource in a warning without inventing a diff for it, and exit 0

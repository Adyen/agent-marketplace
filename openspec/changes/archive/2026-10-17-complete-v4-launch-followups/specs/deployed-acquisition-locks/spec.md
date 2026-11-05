## MODIFIED Requirements

### Requirement: Writable locks record resource-free Marketplace registrations

A v4 source that registers a Marketplace endpoint without selecting resources SHALL be readable without acquisition evidence. A writing `lock` or `sync` for a selected user, repository, or personal-local layer SHALL record that endpoint's well-known-verified instance identity in the layer's acquisition lock, creating the lock when absent or appending only the missing identity record to an existing lock while its other Marketplace and Git records remain exact. Read-only dependency modes MUST NOT fetch the identity: a check MUST report the missing record as a required change, and a locked or offline operation MUST refuse it as stale lock evidence naming the layer scope, lock path, and endpoint. When required configuration and lock validation succeeds, `lock --check` SHALL report every selected writable scope with unrecorded resource-free Marketplace endpoint registrations in layer order, rather than only the first scope. Organization locks MUST NOT be written implicitly.

#### Scenario: Writing commands record a registration identity

- **WHEN** `lock` or `sync` runs for a writable layer whose v4 source registers a Marketplace endpoint without resources and no layer lock exists
- **THEN** the layer lock MUST be created with the endpoint's verified instance identity and no resource records
- **AND** the authored configuration MUST remain unchanged

#### Scenario: Existing lock gains only the missing registration identity

- **WHEN** a writable layer lock already records one Marketplace endpoint and the configuration adds a resource-free registration for another endpoint
- **THEN** `lock` MUST append only the new endpoint's identity record while existing records remain unchanged
- **AND** a repeated `lock` or `sync` MUST NOT rewrite the lock

#### Scenario: Read-only modes refuse an unrecorded registration

- **WHEN** a writable layer lock exists but lacks the identity record for a resource-free registration
- **THEN** `sync --check` and `lock --check` MUST report a required lock change, and `sync --locked` and `sync --offline` MUST refuse with stale lock evidence naming the scope, lock path, and endpoint
- **AND** they MUST NOT contact a Marketplace endpoint or write configuration or locks

#### Scenario: Check reports every selected unrecorded registration scope

- **WHEN** valid user, repository, and personal-local layers register resource-free Marketplace endpoints not recorded in their absent or existing acquisition locks
- **THEN** `lock --check` MUST fail with one `lock.change-required` diagnostic per affected selected scope, in user, repository, and personal-local order, naming each lock path and its unrecorded endpoints
- **AND** an explicit `--scope` MUST omit unselected scopes from these diagnostics
- **AND** the check MUST NOT contact Marketplace endpoints or create or change configuration, locks, caches, or durable state
- **AND** the same check MUST succeed without writes or acquisition when every selected registration is recorded

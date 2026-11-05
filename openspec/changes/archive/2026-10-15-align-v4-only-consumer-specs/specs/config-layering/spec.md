## MODIFIED Requirements

### Requirement: v4 Marketplace sources express consumer selections

Consumer configuration SHALL express a Marketplace endpoint registration as a named `marketplace` source in an exact-v4 root or its accepted include tree. Authored selections SHALL use v4 `resources` bound to that source; exact Marketplace installations and their acquisition evidence SHALL be recorded in the owning acquisition lock, not legacy top-level `marketplaces` or installation keys. Legacy top-level installation keys in a v4 root or accepted fragment MUST be refused with `v4.legacy-key`; changing only a legacy root's version sentinel MUST NOT make its selections valid. Consumer roots and explicit fragment versions SHALL follow `consumer-config-format`.

#### Scenario: v4 root rejects unsafe Marketplace source URLs

- **WHEN** a v4 root declares a `marketplace` source whose URL carries credentials, a query string, a fragment, or an encoded unsafe path
- **THEN** the root MUST be rejected

#### Scenario: v4 root rejects legacy installation keys

- **WHEN** a v4 root or its included fragment declares a legacy top-level installation key such as `registries`, `skills`, `collections`, or `marketplaces`
- **THEN** the document MUST be rejected with `v4.legacy-key` without conversion or legacy interpretation

#### Scenario: v4 roots declare Marketplace sources and resources

- **WHEN** an exact-v4 root declares a named `marketplace` source, resource selections bound to that source, and a permitted `allowed_marketplaces` allowlist
- **THEN** loading authored configuration MUST retain the source, selections, and allowlist without requiring remote acquisition
- **AND** exact installation evidence MUST be validated in the owning acquisition lock when acquired resources are used

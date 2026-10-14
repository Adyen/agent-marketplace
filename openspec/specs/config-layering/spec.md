# config-layering Specification

## Purpose
Defines how consumer configuration layers express, validate, and merge Marketplace endpoint registrations, Marketplace installations, and the Marketplace URL allowlist across the organization, user, repository, and personal-local layers.

## Requirements

### Requirement: Consumer Marketplace configuration validation

Consumer configuration SHALL validate each Marketplace endpoint registration and authored resource selection declared by accepted v4 roots or fragments, including personal-local roots. Endpoint identity and exact Marketplace installation evidence obtained from deployed or transient acquisition locks SHALL retain their existing validation; the v4-only consumer format SHALL NOT relax digest, resource-kind, acquisition-mode, or identity checks.

#### Scenario: Marketplace entries are validated

- **WHEN** v4 Marketplace sources, resource selections, and their required acquisition evidence are validated
- **THEN** missing or invalid source URLs, invalid resource kinds or locators, and unknown authored source/request fields MUST be rejected
- **AND** exact installation evidence with missing endpoint identity or invalid digest, kind, or acquisition mode MUST be rejected
- **AND** URLs containing path traversal MUST be rejected

#### Scenario: Marketplace URL allowlist

- **WHEN** Marketplace endpoint URLs are validated against a configured `allowed_marketplaces` allowlist
- **THEN** URLs matching at least one regex pattern MUST be allowed
- **AND** URLs matching none MUST be reported as blocked, listing the blocked URLs
- **AND** an unset allowlist MUST permit all URLs while an empty allowlist MUST deny all non-empty URLs

#### Scenario: Duplicate and reserved Marketplace aliases are rejected

- **WHEN** one v4 document declares duplicate Marketplace source names or a Marketplace registration uses the reserved alias `all`
- **THEN** validation MUST reject the configuration rather than silently discard an entry

#### Scenario: Layered alias accepted for one endpoint identity

- **WHEN** two v4 configuration layers declare the same Marketplace source name for the same canonical endpoint URL and verified instance ID
- **THEN** validation MUST accept both registrations with their layer provenance
- **AND** duplicate source declarations within one document MUST be rejected

#### Scenario: Unsafe identity and endpoint fields are rejected

- **WHEN** a v4 Marketplace source declares a URL carrying credentials, a query, or an encoded unsafe path, or acquired installation evidence declares an instance ID containing whitespace or a slash or an invalid registry slug, or a source name contains a control character
- **THEN** validation MUST reject the unsafe input
- **AND** an otherwise valid equivalent endpoint declared with different case and a trailing slash MUST be accepted

#### Scenario: Personal-local duplicate aliases are rejected at load

- **WHEN** an exact-v4 personal-local root declares duplicate Marketplace source names
- **THEN** loading the root MUST fail with a coded duplicate-declaration diagnostic

### Requirement: Consumer Marketplace configuration merge

Consumer configuration SHALL merge Marketplace endpoint registrations and the `allowed_marketplaces` allowlist across valid organization, user-personal, repository-committed, and personal-local layers, and across a layer's accepted v4 include tree, preserving per-layer provenance and layer-specific field restrictions. Sources and resource requests SHALL retain their layer bindings and existing ordered selection decisions; the format restriction SHALL NOT change include or allowlist authority.

#### Scenario: Layered merge retains per-layer provenance and first-set allowlist

- **WHEN** accepted v4 configuration layers are merged in precedence order
- **THEN** Marketplace registrations from every layer MUST be retained in layer order, each recording the layer it came from
- **AND** the first permitted layer that sets `allowed_marketplaces` MUST determine the effective allowlist, so a higher-precedence layer cannot relax it

#### Scenario: Allowed Marketplace patterns follow include authority in a v4 root

- **WHEN** a v4 root and an accepted included fragment both declare `allowed_marketplaces`
- **THEN** the fragment's declaration MUST determine the layer's allowlist, and the containing root MUST NOT override it

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
### Requirement: Organization-only GUI release check setting

A top-level `gui` mapping with boolean `version_check` SHALL affect GUI release availability checks only when it is declared in Consumer organization configuration. If the organization value is absent, the default SHALL enable release checking. A `gui.version_check` value in user-personal, repository-committed, or personal-local configuration SHALL be ignored, SHALL produce a warning diagnostic, and SHALL NOT cause that configuration to be rejected.

#### Scenario: Organization setting controls release availability checks

- **WHEN** Consumer organization configuration sets `gui.version_check` to true or false
- **THEN** the effective GUI release-check setting SHALL match that boolean

#### Scenario: Release availability checks default to enabled

- **WHEN** Consumer organization configuration does not set `gui.version_check`
- **THEN** the effective GUI release-check setting SHALL be enabled

#### Scenario: Lower-layer GUI setting is ignored with a warning

- **WHEN** user-personal, repository-committed, or personal-local configuration declares `gui.version_check`
- **THEN** that value SHALL be ignored
- **AND** a warning diagnostic SHALL be reported
- **AND** the configuration SHALL NOT be rejected

### Requirement: Existing unsupported top-level Harness targets remain inspectable

Configuration readers SHALL retain non-empty string identifiers already present in a root's top-level Harness target set when the running client does not support them, and SHALL report a warning identifying the unsupported setting. Unsupported identifiers SHALL NOT become materialization targets or basic telemetry Harness target attributes. Other malformed configuration and unsupported per-entry Harness target narrowing SHALL retain their existing validation behavior. Initialization and explicit Harness target edits SHALL enforce the supported-target and existing-identifier rules in `session-interactions`.

#### Scenario: Existing unsupported identifier is retained without materialization authority

- **WHEN** a root's top-level Harness target set contains a supported target and an identifier unsupported by the running client
- **THEN** the reader SHALL retain both identifiers and report an unsupported-target warning
- **AND** resolution and materialization SHALL use only supported targets
- **AND** basic telemetry SHALL contain only supported Harness target attributes

#### Scenario: Unsupported-only target set grants no materialization authority

- **WHEN** a root's top-level Harness target set contains no supported target
- **THEN** reading the configuration SHALL retain its identifiers with unsupported-target warnings
- **AND** a target-dependent operation SHALL fail for the missing supported target set without materializing Resource files

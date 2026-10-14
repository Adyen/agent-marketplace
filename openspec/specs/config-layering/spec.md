# config-layering Specification

## Purpose
Defines how consumer configuration layers express, validate, and merge Marketplace endpoint registrations, Marketplace installations, and the Marketplace URL allowlist across the organization, user, repository, and personal-local layers.

## Requirements

### Requirement: Consumer Marketplace configuration expression

Consumer configuration SHALL express a Marketplace endpoint registration as a named `marketplace` source in a v4 root and as a `marketplaces` entry in a v2 or v3 root. A `marketplaces` entry MAY also carry Marketplace installations as `resources` pins. A v4 root SHALL reject legacy top-level installation keys.

#### Scenario: v4 root rejects unsafe Marketplace source URLs

- **WHEN** a v4 root declares a `marketplace` source whose URL carries credentials, a query string, a fragment, or an encoded unsafe path
- **THEN** the root MUST be rejected

#### Scenario: v4 root rejects legacy installation keys

- **WHEN** a v4 root declares a legacy top-level installation key
- **THEN** the root MUST be rejected with a legacy-installation-key diagnostic

#### Scenario: v2 and v3 roots declare Marketplace entries

- **WHEN** a root declaring `apiVersion: agent-marketplace/v2` or `agent-marketplace/v3` declares `marketplaces` entries with `url`, `instance_id`, and optional `resources` pins, and an `allowed_marketplaces` allowlist
- **THEN** loading the root MUST retain the entries and the allowlist
- **AND** a pin with an invalid acquisition value or an unknown field MUST be rejected

### Requirement: Consumer Marketplace configuration validation

Consumer configuration SHALL validate every Marketplace endpoint registration and Marketplace installation per entry, whether the entry is declared in a v4 source, a v2 or v3 root, or a personal-local root.

#### Scenario: Marketplace entries are validated

- **WHEN** `marketplaces` entries are validated
- **THEN** entries missing `url` or `instance_id` MUST be rejected
- **AND** `resources` pins with invalid `digest`, `kind`, or `acquisition` values MUST be rejected
- **AND** URLs containing `..` MUST be rejected as path traversal

#### Scenario: Marketplace URL allowlist

- **WHEN** Marketplace endpoint URLs are validated against a configured `allowed_marketplaces` allowlist
- **THEN** URLs matching at least one regex pattern MUST be allowed
- **AND** URLs matching none MUST be reported as blocked, listing the blocked URLs
- **AND** an unset allowlist MUST permit all URLs while an empty allowlist MUST deny all non-empty URLs

#### Scenario: Duplicate and reserved Marketplace aliases are rejected

- **WHEN** two `marketplaces` entries in one file declare the same alias, or one entry declares the alias `all`
- **THEN** validation MUST reject the configuration

#### Scenario: Layered alias accepted for one endpoint identity

- **WHEN** two configuration layers declare the same alias for the same canonical endpoint URL and instance ID
- **THEN** validation MUST accept both entries
- **AND** two entries in one file MUST be rejected

#### Scenario: Unsafe identity and endpoint fields are rejected

- **WHEN** an entry declares a URL carrying credentials, a query, or an encoded unsafe path, an instance ID containing whitespace or a slash, an alias containing a control character, or a pin with an invalid registry slug
- **THEN** validation MUST reject the entry
- **AND** an equivalent endpoint declared with different case and a trailing slash MUST be accepted

#### Scenario: Personal-local duplicate aliases are rejected at load

- **WHEN** a personal-local root declares two Marketplace entries with the same alias
- **THEN** loading the root MUST fail with a duplicate-alias diagnostic

### Requirement: Consumer Marketplace configuration merge

Consumer configuration SHALL merge Marketplace endpoint registrations and the `allowed_marketplaces` allowlist across the organization, user-personal, repository-committed, and personal-local layers, and across a layer's include tree, preserving per-layer provenance.

#### Scenario: Layered merge retains per-layer provenance and first-set allowlist

- **WHEN** configuration layers are merged in precedence order
- **THEN** Marketplace entries from every layer MUST be retained in layer order, each recording the layer it came from
- **AND** the first layer that sets `allowed_marketplaces` MUST determine the effective allowlist, so a higher-precedence layer cannot relax it

#### Scenario: Allowed Marketplace patterns follow include authority in a v4 root

- **WHEN** a v4 root and an included fragment both declare `allowed_marketplaces`
- **THEN** the fragment's declaration MUST determine the layer's allowlist, and the containing root MUST NOT override it

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

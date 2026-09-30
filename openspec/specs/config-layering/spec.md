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

#### Scenario: Provisional instance entry defers to a verified layered instance

- **WHEN** an organization entry declares a provisional instance ID and another layer declares the same alias at the same canonical endpoint URL with a verified instance ID
- **THEN** validation MUST accept both entries
- **AND** the same alias at a different endpoint URL, both entries in one file, or two verified instances at one endpoint MUST be rejected

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

## MODIFIED Requirements

### Requirement: OpenAPI contract

The repository SHALL contain a canonical OpenAPI 3.0.3 document describing the
public Marketplace API.

#### Scenario: Required paths and schemas are present

- **WHEN** `api/openapi.yaml` is parsed as YAML
- **THEN** it MUST contain at least the discovery, catalog, search, registry,
  resource, revision, manifest, assessment, inspection, content, artifact,
  browser, and health paths:
  - `/.well-known/agent-marketplace.json`
  - `/api/v1/capabilities`, `/api/v1/catalog`, `/api/v1/instance`, `/api/v1/search`
  - `/api/v1/registries`, `/api/v1/registries/{registry}`,
    `/api/v1/registries/{registry}/history`,
    `/api/v1/registries/{registry}/occurrences/{commit}`
  - `/api/v1/resources/{registry}/{kind}/{name}`, its `/revisions` and
    `/revisions/{revision}` children, and the revision's `/inspection`,
    `/inspection/content`, and `/inspection/content/{digest}` children
  - `/api/v1/manifests/{registry-revision}`
  - `/api/v1/assessments/sha256/{digest}`, `/api/v1/inspection/sha256/{digest}`,
    `/api/v1/content/sha256/{digest}`, `/api/v1/artifacts/sha256/{digest}`
  - `/r/{registry}/{kind}/{name}` and `/r/{registry}/{kind}/{name}/revisions/{revision}`
  - `/healthz`
- **AND** it MUST contain the schemas `Capabilities`, `WellKnown`, `Marketplace`,
  `Registry`, `RegistryOccurrence`, `ExactRegistryOccurrence`, `SearchResult`,
  `ResourceListItem`, `ResourceRevision`, `ResourceAssessment`,
  `AssessmentFinding`, `ResourceOccurrence`, `Revocation`, `ResourceCoordinate`,
  `InspectionManifest`, `InspectionFile`, and the `Catalog*` projection schemas

#### Scenario: Go constants match the OpenAPI enum

- **WHEN** `DefaultCapabilities()` is called
- **THEN** `APIVersions` MUST be `["v1"]`
- **AND** `ArtifactSchemaVersions` MUST be `["v1"]`
- **AND** `ResourceKinds` MUST match the enum values declared in the OpenAPI document

### Requirement: Consumer config fields

Consumer configuration SHALL support Marketplace endpoint entries
(`marketplaces` in the legacy v2/v3 schema) and an `allowed_marketplaces`
allowlist, validated per entry and merged across configuration layers.

#### Scenario: Marketplace entries are validated

- **WHEN** `ValidateMarketplaceEntries` is called
- **THEN** entries missing `url` or `instance_id` MUST be rejected
- **AND** `resources` pins with invalid `digest`, `kind`, or `acquisition` values
  MUST be rejected
- **AND** URLs containing `..` MUST be rejected as path traversal

#### Scenario: Marketplace URL allowlist

- **WHEN** `ValidateMarketplaceURLs` is called with a non-nil `allowed_marketplaces`
  allowlist
- **THEN** URLs matching at least one regex pattern MUST be allowed
- **AND** URLs matching none MUST be reported in a `MarketplaceURLBlockedError`
- **AND** a `nil` allowlist MUST permit all URLs while an empty allowlist MUST deny
  all non-empty URLs

#### Scenario: Layered config wiring

- **WHEN** configuration layers are merged in precedence order
- **THEN** Marketplace entries from every layer MUST be retained in layer order,
  each recording the layer it came from
- **AND** the first layer that sets `allowed_marketplaces` MUST determine the
  effective allowlist, so a higher-precedence layer cannot relax it

### Requirement: Canonical artifact format

`pkg/harness-compilation/artifact` SHALL implement Marketplace artifact schema v1
as a deterministic `tar.gz` archive containing:

- `manifest.json` with schema, kind, name, revision, digests, provenance, and
  optional capabilities/findings
- `resource/<kind>.json` canonical payload
- optional `assets/<relative-path>` files
- optional `licenses/<relative-path>` files
- optional `support/<url-escaped-relative-path>` files carrying the declared
  support bytes and executable modes of a mirrored skill; declared paths may
  be flat or nested and are url-escaped into member names

`support/` members remain within artifact schema v1 but change the archive
digest; because `archive_digest` is immutable per content digest, they can
only ship under a new content revision. Clients predating `support/` members
fail canonical digest validation on such archives rather than silently
installing them. An already-published artifact that declares support files but
does not carry `support/` members cannot be repaired in place; consumers MUST
fail closed, preserving any last-good installation, until a new content
revision or an index-only source supplies the bytes.

#### Scenario: Canonical artifact round-trip

- **WHEN** a `CanonicalArtifact` is built from any selectable IR resource
  (`skill`, `command`, `subagent`, `hook`, or `mcpserver`)
- **THEN** `Encode` MUST return a deterministic `tar.gz` archive and an archive digest
- **AND** `Decode` MUST restore the manifest, payload, assets, licenses, and
  support files
- **AND** `VerifyContentDigest`, `VerifyArchiveDigest`, and `ValidateArchive`
  MUST pass for the decoded artifact

#### Scenario: Payload contains resource name

- **WHEN** `FromIR` is called for a resource
- **THEN** the canonical `resource/<kind>.json` payload MUST contain the resource name

#### Scenario: Security rejection during decode

- **WHEN** a `tar.gz` containing absolute paths, `..` path components, symlinks,
  hard links, or device/special files is decoded
- **THEN** `Decode` MUST return an error before the unsafe entry is accepted

#### Scenario: Oversized resource rejection

- **WHEN** `ValidateArchive` is called with a payload or asset exceeding the
  configured `ArchiveLimits`
- **THEN** it MUST return an error identifying the oversized resource

#### Scenario: Archive digest resolves the self-reference problem

- **WHEN** `VerifyArchiveDigest` is called on an artifact whose manifest
  `archive_digest` does not match its canonical archive
- **THEN** it MUST fail with an archive digest mismatch
- **AND** when raw archive bytes are supplied that differ from the canonical
  archive, it MUST fail even if the manifest digest is correct
- **AND** an untampered encoded artifact MUST verify

## REMOVED Requirements

### Requirement: Test and benchmark coverage

**Reason**: States which tests and benchmarks exist rather than how the system
behaves; scenario-to-test traceability in `spec-governance` supersedes it.
**Migration**: Coverage is enforced by `// spec:` annotations on every
remaining `marketplace-protocols` scenario.

# marketplace-protocols Specification

## Purpose

Define the public Marketplace protocol surface shared by the server, CLI, and
generated clients: the OpenAPI contract, selectable resource kinds, analyzer
finding codes, consumer Marketplace configuration, and the canonical artifact
and collection manifest formats.

## Requirements

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

### Requirement: Go API kinds and wire types

`pkg/marketplace/api` SHALL be the runtime source of truth for Marketplace
selectable kinds, version/mode constants, and wire structs shared with the server,
CLI, and generated clients.

#### Scenario: Selectable resource kinds

- **WHEN** `SupportedResourceKinds` is inspected
- **THEN** it MUST contain `skill`, `command`, `subagent`, `hook`, `mcpserver`,
  `collection` in that order
- **AND** `IsMarketplaceKind` MUST return `true` for those values and `false`
  for non-selectable kinds such as `rule`

#### Scenario: Bidirectional kind conversion

- **WHEN** `api.ToIR` and `api.FromIR` are called with valid Marketplace kinds
- **THEN** they MUST map to/from `ir.KindSkill`, `ir.KindCommand`,
  `ir.KindSubagent`, `ir.KindHook`, and `ir.KindMcpServer`
- **AND** `api.ToIR` MUST return `false` for `collection` (virtual) and unknown kinds

### Requirement: Analyzer finding codes

`pkg/marketplace/analyze` SHALL define stable security-analyzer finding codes and
ship a Markdown catalog that documents each code.

#### Scenario: Code catalog parity

- **WHEN** `FindingCodes()` is called
- **THEN** every returned code MUST appear in `pkg/marketplace/analyze/codes.md`
- **AND** every code MUST use the `marketplace.find.` prefix
- **AND** the code set MUST contain no duplicates

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

### Requirement: Collection manifest format

`pkg/harness-compilation/artifact` SHALL implement a standalone, deterministic JSON
manifest for virtual collections. Collection manifests are not tar archives.

#### Scenario: Collection manifest round-trip

- **WHEN** a `CollectionManifest` is built from an IR collection
- **THEN** `EncodeCollectionManifest` MUST return a compact JSON payload and a
  content digest
- **AND** `DecodeCollectionManifest` followed by `VerifyCollectionManifest` MUST
  reproduce and verify the digest

#### Scenario: Deterministic ordering

- **WHEN** members and includes are encoded
- **THEN** `members` MUST be sorted by `kind` then `name`
- **AND** `includes` MUST be sorted lexicographically
- **AND** repeated encoding MUST produce identical bytes and digests

#### Scenario: Content digest excludes the digest field

- **WHEN** a collection manifest payload is verified
- **THEN** `VerifyCollectionManifest` MUST recompute the digest with
  `content_digest` cleared
- **AND** a tampered `content_digest` value MUST fail verification

# Marketplace protocols and contracts

This specification freezes the public protocol surface introduced by bead
`agent-marketplace-pf99.1`. See `openspec/changes/agent-marketplace-pf99.1/design.md`
and `openspec/changes/agent-marketplace-pf99.1/proposal.md` for motivation, scope,
and design decisions.

## Requirements

### Requirement: OpenAPI contract

The repository SHALL contain a canonical OpenAPI 3.0.3 document describing the
public Marketplace API.

#### Scenario: Required paths and schemas are present

- **WHEN** `api/openapi.yaml` is parsed as YAML
- **THEN** it MUST contain the paths:
  - `/.well-known/agent-marketplace.json`
  - `/api/v1/capabilities`
  - `/api/v1/resources`
  - `/api/v1/artifacts/{kind}/{name}`
- **AND** it MUST contain the schemas:
  - `Capabilities`
  - `WellKnown`
  - `ResourceListItem`

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

The v2 canonical config schema SHALL support `marketplaces` and
`allowed_marketplaces` fields. These fields are additive; the canonical `apiVersion`
remains `agent-marketplace/v2` and `agent-marketplace/v3` is kept as a compatibility
alias.

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

- **WHEN** `Config`, `LocalConfig`, the v3 loader, the merge step, and the
  effective config resolver are inspected
- **THEN** `marketplaces` and `allowed_marketplaces` MUST be present in the
  per-layer allowlists and merged effective config
- **AND** `marketplaces` entries MUST accumulate across layers and be stamped with
  their `Source`
- **AND** `allowed_marketplaces` MUST use first-non-nil-wins precedence

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

- **WHEN** an artifact is encoded
- **THEN** the manifest's `archive_digest` MUST be computed over a canonical form
  of the archive in which `archive_digest` is replaced by a fixed-length placeholder
- **AND** `VerifyArchiveDigest` MUST recompute the digest using the same placeholder
  form and, when raw bytes are supplied, MUST also confirm the raw bytes match the
  canonical final archive

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

### Requirement: Test and benchmark coverage

All protocol code SHALL have unit tests and hot-path benchmarks.

#### Scenario: Artifact coverage

- **WHEN** `pkg/harness-compilation/artifact` is tested
- **THEN** there MUST be tests for round-trips of all selectable kinds,
  determinism, `FromIR` payload content, and rejection of unsafe paths, bad
  digests, and oversized resources
- **AND** there MUST be benchmarks for `Encode`, `Decode`, `FromIR`, and
  `VerifyArchiveDigest`

#### Scenario: Collection coverage

- **WHEN** `pkg/harness-compilation/artifact` is tested
- **THEN** there MUST be tests for `CollectionFromIR` across all kinds,
  deterministic encoding, validation of invalid schemas/kinds, and
  `content_digest` verification

# marketplace-api Specification

## Purpose
Defines the public Marketplace protocol surface shared by the server, CLI, and generated clients: the OpenAPI contract, the capabilities and well-known discovery documents, the wire vocabulary of selectable resource kinds, the analyzer finding-code catalog, and the artifact and collection-manifest wire formats.
## Requirements
### Requirement: OpenAPI contract

The repository SHALL contain a canonical OpenAPI 3.0.3 document describing the
public Marketplace API.

#### Scenario: OpenAPI document declares the Marketplace paths and schemas

- **WHEN** the canonical OpenAPI document at `api/openapi.yaml` is parsed as
  YAML
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

#### Scenario: Inspection content routes serve the public media types

- **WHEN** the canonical OpenAPI document declares the success responses of
  the inspection content routes (`/api/v1/content/sha256/{digest}`, a
  revision's `/inspection/content`, and its `/inspection/content/{digest}`
  child)
- **THEN** their media types MUST be exactly the fixed public media-type set
  of the inspection projection

### Requirement: Capabilities document and wire vocabulary

A Marketplace SHALL serve a capabilities document declaring its API major
versions, artifact schema versions, selectable resource kinds, acquisition
modes, and optional server features. The server, CLI, and generated clients
SHALL share one wire vocabulary of selectable resource kinds.

#### Scenario: Capabilities document matches the OpenAPI contract

- **WHEN** the capabilities document of a Marketplace instance is produced
- **THEN** it MUST declare the API major versions `["v1"]`
- **AND** the artifact schema versions `["v1"]`
- **AND** its resource kinds MUST match the `resource_kinds` enum declared in
  the canonical OpenAPI document

#### Scenario: Capabilities document declares acquisition modes and server features

- **WHEN** the capabilities document of a Marketplace instance is produced
- **THEN** it MUST declare the index-only and mirror acquisition modes
- **AND** it MUST advertise the catalog projection contract
  (`catalog-projection-v1alpha1`) as a server feature

#### Scenario: Selectable resource kinds

- **WHEN** the selectable resource kind vocabulary is inspected
- **THEN** it MUST contain `skill`, `command`, `subagent`, `hook`, `mcpserver`,
  `collection` in that order
- **AND** exactly those values MUST be treated as Marketplace-selectable kinds
  while non-selectable kinds such as `rule` MUST be rejected

#### Scenario: Bidirectional kind conversion

- **WHEN** Marketplace wire kind values are converted to and from the
  harness-neutral resource representation
- **THEN** the `skill`, `command`, `subagent`, `hook`, and `mcpserver` kinds
  MUST map in both directions
- **AND** conversion into the harness-neutral representation MUST reject
  the virtual `collection` kind and unknown values

### Requirement: Well-known discovery document

A Marketplace SHALL serve a well-known discovery document that maps its
browser routes back to the API base and instance ID, exposing browser route
templates only when a browser base is configured.

#### Scenario: Well-known document exposes browser templates only when configured

- **WHEN** the well-known discovery document is produced without a browser
  base
- **THEN** it MUST omit the browser base and browser route templates
- **AND WHEN** it is produced with a browser base
- **THEN** it MUST expose the browser base plus the latest and exact browser
  route templates
- **AND** the canonical OpenAPI document MUST keep the browser fields of the
  well-known schema optional so existing clients remain conformant

### Requirement: Analyzer finding codes

The Marketplace security-analyzer finding-code catalog SHALL define stable
finding codes, and a Markdown catalog shipped beside the analyzer SHALL
document each code.

#### Scenario: Code catalog parity

- **WHEN** the finding-code catalog is listed
- **THEN** every finding code MUST appear in the Markdown catalog
- **AND** every code MUST use the `marketplace.find.` prefix
- **AND** the code set MUST contain no duplicates

### Requirement: Canonical artifact format

A resource artifact SHALL be a deterministic `tar.gz` archive under artifact
schema v1 containing:

- `manifest.json` with schema, kind, name, revision, digests, provenance, and
  optional capabilities/findings
- `resource/<kind>.json` canonical payload
- optional `assets/<relative-path>` files
- optional `licenses/<relative-path>` files
- optional `support/<url-escaped-relative-path>` files carrying the declared
  support bytes and executable modes of a mirrored skill or hook; declared paths
  may be flat or nested and are url-escaped into member names

`support/` members remain within artifact schema v1 but change the archive
digest; because `archive_digest` is immutable per content digest, they can
only ship under a new content revision. Clients predating `support/` members
fail canonical digest validation on such archives rather than silently
installing them. An already-published artifact that declares support files but
does not carry `support/` members cannot be repaired in place; consumers MUST
fail closed, preserving any last-good installation, until a new content
revision or an index-only source supplies the bytes.

#### Scenario: Canonical artifact round-trip

- **WHEN** a resource artifact is built from any materialized resource kind
  (`skill`, `command`, `subagent`, `hook`, or `mcpserver`)
- **THEN** encoding MUST produce a deterministic `tar.gz` archive and an
  archive digest
- **AND** decoding MUST restore the manifest, payload, assets, licenses, and
  support files
- **AND** content-digest, archive-digest, and archive validation MUST pass for
  the decoded artifact

#### Scenario: Payload contains resource name

- **WHEN** a resource artifact is built from a resource
- **THEN** the canonical `resource/<kind>.json` payload MUST contain the
  resource name

#### Scenario: Security rejection during decode

- **WHEN** a `tar.gz` containing absolute paths, `..` path components,
  symlinks, hard links, or device/special files is decoded
- **THEN** decoding MUST fail before the unsafe entry is accepted

#### Scenario: Oversized resource rejection

- **WHEN** archive validation runs with a payload or asset exceeding the
  configured archive limits
- **THEN** it MUST fail with an error identifying the oversized resource

#### Scenario: Archive digest verification catches archive tampering

- **WHEN** archive digest verification runs on a resource artifact whose
  manifest `archive_digest` does not match its canonical archive
- **THEN** it MUST fail with an archive digest mismatch
- **AND** when raw archive bytes that differ from the canonical archive are
  supplied, it MUST fail even if the manifest digest is correct
- **AND** an untampered encoded artifact MUST verify

### Requirement: Collection manifest format

A collection manifest SHALL be a standalone, deterministic JSON manifest for
virtual collection revisions. Collection manifests are not tar archives.

#### Scenario: Collection manifest round-trip

- **WHEN** a collection manifest is built from a collection
- **THEN** encoding MUST produce a compact JSON payload and a content digest
- **AND** decoding followed by verification MUST reproduce and verify the
  digest

#### Scenario: Deterministic ordering

- **WHEN** members and includes are encoded
- **THEN** `members` MUST be sorted by `kind` then `name`
- **AND** `includes` MUST be sorted lexicographically
- **AND** repeated encoding MUST produce identical bytes and digests

#### Scenario: Content digest excludes the digest field

- **WHEN** a collection manifest payload is verified
- **THEN** verification MUST recompute the digest with `content_digest`
  cleared
- **AND** a tampered `content_digest` value MUST fail verification

### Requirement: Mirrored hooks carry integrity-bound support files

Mirror acquisition MUST package the adjacent support files selected by the
source-layout hook materialization policy, excluding native aggregate
definitions. Their relative paths, bytes, and executable modes MUST be bound
into the canonical hook content revision. Consumers MUST verify declaration
membership and integrity before staging and on the bytes used for materialization.
Existing published archives MUST NOT be repaired in place. The adjacent-file
policy selects flat siblings, not nested directories or other plugin directories;
unsupported expressions MUST have a diagnostic and an achievable remedy.

#### Scenario: Mirrored hook support installation

- **GIVEN** a mirrored hook whose command references a plugin-root script with adjacent support files
- **WHEN** a consumer resolves and materializes it at repository or machine scope
- **THEN** the support bytes and executable mode MUST be installed as regular files in the target's declared per-source, per-hook support directory
- **AND** the command MUST refer to that script through the compiler's support-base API, and verified cached resolution MUST work without network access

#### Scenario: Mirrored hook support integrity

- **GIVEN** a mirrored hook with declared support files
- **WHEN** a declared file is missing, its bytes or executable mode differ, an undeclared support file is present, or a declaration names a native definition
- **THEN** resolution MUST refuse before staging or materializing the hook

#### Scenario: Mirrored hook support safe publication

- **GIVEN** hook support files in a registry repository
- **WHEN** publication reads those files
- **THEN** it MUST use bounded descriptor-based reads anchored at the acquisition root and refuse symlinks, multiply linked files, special files, and oversized files without altering external bytes

#### Scenario: Mirrored hook support revision changes

- **GIVEN** a published hook with support files
- **WHEN** a support file's bytes or executable mode change
- **THEN** publication MUST produce a new content revision without changing the old artifact

#### Scenario: Legacy mirrored script hook refusal

- **GIVEN** a legacy mirrored hook referencing an adjacent plugin-root script without packaged support files
- **WHEN** a consumer resolves the hook
- **THEN** resolution MUST fail with `marketplace.hook.support-missing` instead of reporting a healthy hook
- **AND** legacy support-free hooks without plugin-root asset references MUST remain usable

#### Scenario: Mirrored hook support materialization refusal

- **GIVEN** declared hook support is unavailable, unsafe, tampered, names a native definition, or comes from index-only acquisition
- **WHEN** materialization consumes the hook
- **THEN** it MUST refuse with `materialize.support-source.missing` before emitting the hook or support output, preserving the previous installation

#### Scenario: Mirrored hook support shell safety

- **GIVEN** an installed support path contains spaces or shell metacharacters
- **WHEN** the compiler replaces an unquoted or double-quoted plugin-root reference
- **THEN** the command MUST execute the installed script without executing path text as shell code

#### Scenario: Mirrored hook support unowned destination refusal

- **GIVEN** an installed hook support destination lacks ownership proof
- **WHEN** the same mirrored hook is selected again
- **THEN** materialization MUST refuse with `hook.support-path-unrepresentable` before updating its aggregate, even when support bytes are identical

#### Scenario: Mirrored hook support acquisition containment

- **GIVEN** a resource directory traverses a symlink beneath the acquisition root
- **WHEN** publication reads declared support
- **THEN** it MUST refuse with `marketplace.ingest.hook-support-unsafe` and leave external bytes unchanged

#### Scenario: Mirrored hook support publication diagnostic

- **GIVEN** a mirrored hook references an asset the adjacent-file policy cannot package
- **WHEN** its registry is published
- **THEN** publication MUST report `marketplace.ingest.hook-support-unavailable`, while support-free hooks remain publishable without that diagnostic

#### Scenario: Mirrored hook support unsupported reference refusal

- **GIVEN** a mirrored command references a nested directory, another plugin directory, or a dynamic plugin-root expression
- **WHEN** a consumer resolves the hook
- **THEN** it MUST refuse with `marketplace.hook.support-unsupported` and recommend retaining the Git source rather than refreshing an unsatisfiable revision

#### Scenario: Mirrored hook support stable offline refusal

- **GIVEN** verified cached archive evidence produced a deterministic missing or unsupported support refusal online
- **WHEN** the consumer resolves it offline
- **THEN** it MUST retain the same refusal code without downloading the artifact again

#### Scenario: Mirrored hook support cache independent installation

- **GIVEN** a mirrored hook has been installed with support files
- **WHEN** the Marketplace cache is deleted
- **THEN** its command MUST still execute from installed regular support files on every target and scope
- **AND** deselection MUST remove its owned support files

#### Scenario: Mirrored hook support independent sources

- **GIVEN** two mirrored sources supply hooks with the same name and different same-named support scripts
- **WHEN** both hooks are materialized
- **THEN** each command MUST execute its own installed script without a support-file collision
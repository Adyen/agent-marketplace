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
  support bytes and executable modes of a mirrored skill; declared paths may
  be flat or nested and are url-escaped into member names

`support/` members remain within artifact schema v1 but change the archive
digest. The archive digest is encoding evidence, not revision identity: a
revision whose content digest is unchanged MAY carry a repaired archive
digest produced by a newer artifact encoding, and the Marketplace repairs
such a revision in place while retaining the previously published artifact
(see the `marketplace-publication` capability). Clients predating `support/`
members fail canonical digest validation on such archives rather than
silently installing them. A client whose locked Marketplace record pins an
archive digest that the current revision metadata for the same content digest
no longer carries MUST refresh the lock's archive evidence and re-acquire
the resource artifact, reporting the refresh, rather than failing closed; a
last-good installation is preserved when re-acquisition fails.

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

#### Scenario: Same-content revision carries a repaired archive digest

- **WHEN** a published resource revision's artifact encoding changes so that
  the same content digest yields a new archive digest
- **THEN** the revision MUST remain identified by its content digest
- **AND** the Marketplace MUST serve the repaired archive digest as the
  revision's archive digest
- **AND** the previously published artifact MUST remain content-addressed and
  downloadable

#### Scenario: Locked archive evidence refreshes on identical content

- **WHEN** a locked Marketplace record pins an archive digest and the fetched
  revision metadata for the identical content digest carries a different
  archive digest
- **THEN** the client MUST refresh the lock's archive evidence to the fetched
  archive digest and re-acquire the resource artifact
- **AND** the client MUST report the refresh rather than fail resolution
- **AND** the lock's content digest for the resource MUST remain unchanged

#### Scenario: TUI sync and update report the refresh non-fatally

- **WHEN** the interactive TUI runs a sync or update and a locked
  Marketplace record pins an archive digest that the fetched revision
  metadata for the identical content digest no longer carries
- **THEN** the TUI MUST persist the refreshed archive evidence to the
  writable lock and report the refresh as a non-fatal
  `marketplace.archive-evidence-refreshed` warning visible in the
  diagnostics view
- **AND** a healthy sync or update without drift MUST complete without a
  refresh warning or lock write
- **AND** a failed re-acquisition or a lock managed by a read-only scope
  MUST fail closed, preserving the last-good installation and the
  unchanged lock

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

### Requirement: Current reported installation counts

The Marketplace SHALL derive adoption from distinct reporting Installation IDs whose latest complete inventory observation is no older than 30 days. A resource SHALL count at most once per Installation ID and resource coordinate, including observations of the same coordinate through both Git and Marketplace acquisition. Legacy Git observations SHALL contribute only through unambiguous matches between telemetry registry identities and Marketplace registry origins. Explicit hosted observations SHALL contribute only when their Marketplace instance ID matches the serving Marketplace and their registry ID is known to that Marketplace. Counts describe enabled configuration, not verified materialization or unique users. Legitimate rule observations SHALL NOT contribute to adoption or invalidate counts for Marketplace-selectable resources. Superseded inventories SHALL NOT consume the metrics response budget when the latest-inventory projection fits within that budget.

#### Scenario: Latest inventory excludes removed and stale resources

- **WHEN** installation metrics include repeated reports, older inventories containing removed resources, and inventory observations older than 30 days
- **THEN** only resources present in each fresh latest inventory SHALL contribute once per reporting Installation ID
- **AND** fresh scrape timestamps SHALL NOT make an old inventory observation fresh

#### Scenario: Registry identity matching does not conflate resources

- **WHEN** installation metrics contain the same resource name in different registries, multiple origins belonging to one registry, or an origin matching multiple registry identities
- **THEN** counts SHALL remain separate for different resource coordinates and duplicate observations from one installation across one registry's origins SHALL count once
- **AND** ambiguous origins SHALL NOT contribute or produce fabricated zero counts

#### Scenario: Rule observations preserve selectable resource adoption

- **WHEN** a fresh latest inventory contains valid rule observations alongside Marketplace-selectable resources
- **THEN** installation-ordered search SHALL return counts for those selectable resources without counting rules
- **AND** malformed observations SHALL still trigger the existing metrics fallback

#### Scenario: Superseded inventory history does not exhaust the response budget

- **WHEN** superseded inventory history exceeds the metrics response budget but the latest-inventory projection fits within that budget
- **THEN** installation-ordered search SHALL return counts derived from the latest inventories
- **AND** equally recent conflicting inventory fingerprints SHALL remain ambiguous and SHALL NOT contribute
- **AND** responses exceeding the budget even after projection SHALL retain the existing discovery fallback

#### Scenario: Hosted adoption is isolated and deduplicated

- **WHEN** a fresh complete inventory reports hosted resource coordinates from the serving Marketplace, another Marketplace with the same registry and resource names, and a Git observation matching an already reported hosted coordinate
- **THEN** installation-ordered search SHALL count only the serving Marketplace's coordinates and count the shared Git and hosted coordinate once per Installation ID
- **AND** different registry IDs SHALL remain distinct even when resource names match
- **AND** a newer complete generation without the resource SHALL remove its contribution

### Requirement: Optional popularity search order

The Marketplace search API SHALL accept `sort=installations` to order the complete filtered result by descending reported installation count before pagination. For text searches, existing relevance SHALL precede installation count. Equal ranks SHALL retain deterministic registry identity, resource kind, and name ordering. Requests omitting `sort` SHALL retain existing ordering and SHALL NOT require a metrics backend. Unsupported sort values SHALL return HTTP 400.

#### Scenario: Popularity applies before pagination with relevant search matches

- **WHEN** a client requests installation ordering with pagination, text, kind, or registry filters
- **THEN** installation rank SHALL apply to the full filtered result before selecting a page, after text relevance when present
- **AND** ties SHALL be deterministic and existing filter semantics SHALL remain unchanged

#### Scenario: Existing clients and invalid sorting remain predictable

- **WHEN** a client omits `sort` or supplies an unsupported sort value
- **THEN** an omitted value SHALL preserve existing search ordering without contacting metrics
- **AND** an unsupported value SHALL return HTTP 400

### Requirement: Optional bounded installation metrics connection

Marketplace operator configuration SHALL optionally select a Prometheus-compatible metrics endpoint, deployment label selectors, and an environment-sourced bearer credential. The endpoint and credentials SHALL remain server-side. Metrics requests SHALL have bounded execution and an eight-MiB response-size limit and SHALL NOT forward credentials through redirects. Each API process SHALL refresh aggregate counts asynchronously at startup and proactively without requiring search traffic. Search SHALL use only a complete, identity-compatible, unexpired aggregate and SHALL NOT fetch telemetry or wait for refresh. Aggregate expiry SHALL be the earlier of its evaluation time plus the configured maximum age and its inventory validity horizon. A failed proactive refresh SHALL retain a still-fresh compatible last-good aggregate only until its original expiry. Disabled, empty, unavailable, malformed, expired or identity-incompatible counts SHALL preserve existing search ordering and omit installation counts. Public responses and logs SHALL NOT disclose Installation IDs or metrics credentials.

#### Scenario: Metrics unavailable does not break resource discovery

- **WHEN** metrics are disabled, no usable aggregate exists yet, or an empty, failed, malformed or expired metrics result leaves no usable aggregate
- **THEN** resource search SHALL remain available in its existing order without waiting for telemetry
- **AND** public resource items SHALL omit installation counts rather than fabricate zero values or reuse expired counts

#### Scenario: Metrics configuration and requests preserve the server boundary

- **WHEN** an operator configures metrics or the Marketplace queries its metrics endpoint
- **THEN** invalid endpoint or selector configuration SHALL be rejected, requests and responses SHALL be bounded, and redirects SHALL NOT receive credentials
- **AND** searches SHALL reuse a usable aggregate without themselves issuing telemetry requests
- **AND** public responses and logs SHALL omit Installation IDs and credentials

#### Scenario: Searches remain responsive during refresh

- **WHEN** a telemetry refresh is blocked while concurrent clients request installation-ordered search
- **THEN** those searches SHALL finish without waiting for the blocked refresh, using a compatible fresh aggregate or existing ordering with counts omitted
- **AND** cancelling a search SHALL NOT cancel or suppress the background refresh

#### Scenario: Fresh last-good counts survive a failed proactive refresh

- **WHEN** a proactive refresh fails while a compatible last-good aggregate remains fresh
- **THEN** searches SHALL continue using that aggregate only until its original expiry
- **AND** after expiry searches SHALL use existing ordering and omit counts without waiting for a retry

#### Scenario: Registry identity and inventory expiry prevent unsafe counts

- **WHEN** registry identities or origins change, an inventory reaches its validity horizon, or a candidate expires before publication
- **THEN** searches SHALL NOT use incompatible or expired counts, including when registry metadata changes during a telemetry fetch
- **AND** background refresh SHALL recover using current registry metadata without a busy retry loop

### Requirement: Installation metrics timing is independently configurable

Marketplace operator configuration SHALL accept optional `installation_metrics.refresh.interval`, `installation_metrics.refresh.retry_interval`, `installation_metrics.refresh.jitter`, `installation_metrics.max_age` and `installation_metrics.timeout` with defaults of four minutes, thirty seconds, fifteen seconds, five minutes and two seconds respectively. Successful refresh scheduling SHALL use the evaluation time plus interval and additional jitter between zero and its configured bound, or just after an earlier inventory validity horizon. A refresh loop SHALL have at most one active attempt. Unavailable-count or identity-change search hints SHALL be non-blocking and coalesced, SHALL NOT repeat work for a still-fresh compatible aggregate, and SHALL NOT bypass the minimum failure retry delay measured from attempt completion. Construction and handler access SHALL NOT start refresh work; API shutdown SHALL cancel outstanding refresh work and await its termination. Metrics without a configured URL SHALL remain disabled and SHALL NOT affect readiness. Registry reindex timing SHALL remain independent.

#### Scenario: Existing configuration uses defaults independently of reindexing

- **WHEN** an operator leaves the new installation metrics timing settings absent or changes only registry reindex timing
- **THEN** installation refresh SHALL use its own four-minute interval, thirty-second retry, fifteen-second jitter, five-minute maximum age and two-second timeout
- **AND** leaving the metrics URL absent SHALL start no telemetry worker

#### Scenario: Timing overrides and explicit zero jitter are honored

- **WHEN** an operator supplies valid installation metrics duration overrides, including explicit zero jitter
- **THEN** refresh scheduling, failure retry, snapshot expiry and backend timeout SHALL use those overrides
- **AND** explicit zero jitter SHALL disable additional scheduling delay

#### Scenario: Invalid timing configuration is rejected

- **WHEN** an operator supplies non-positive interval, retry interval, maximum age or timeout, negative jitter, unknown timing keys, or interval plus jitter plus timeout greater than or equal to maximum age
- **THEN** configuration loading SHALL reject the settings instead of starting an unsafe refresh schedule

#### Scenario: Background scheduling coalesces hints and respects backoff

- **WHEN** search hints arrive concurrently during startup, an active refresh, or a failed attempt's retry delay
- **THEN** at most one refresh attempt SHALL be active and hints SHALL NOT bypass the retry delay or repeat work for a compatible fresh aggregate
- **AND** refreshes and retries SHALL proceed without further search traffic

#### Scenario: API lifecycle owns refresh work

- **WHEN** an API process starts with metrics configured and later shuts down during backend I/O
- **THEN** initial refresh SHALL run asynchronously without gating readiness or search
- **AND** shutdown SHALL cancel backend I/O and await the worker without search cancellation owning that lifecycle
- **AND** constructing an API handler without starting the process SHALL issue no telemetry request

### Requirement: Complete hosted inventory reporting

Detailed primary telemetry SHALL represent resolved Marketplace resources using explicit source.kind=`marketplace`, marketplace.id, registry.id, resource.kind and canonical resource.name attributes. Legacy Git and directory observations SHALL remain readable without the new attributes. Root and pathful Marketplace endpoints, mirror acquisition, index-only acquisition and installed aliases SHALL NOT cause valid complete inventories to be omitted. Inventory registry and resource points SHALL retain hosted identity without exporting endpoint credentials or private source paths. Attribute values SHALL obey the existing 512-byte UTF-8 bound and total inventories SHALL obey the existing 1000-registry and 10000-resource bounds. Unknown source kinds, incoherent hosted identity pairs, malformed identifiers and exceeded bounds SHALL retain whole-generation omission or metrics-unavailable fallback rather than silently publishing incomplete data. Basic and disabled telemetry SHALL NOT emit resource inventories.

#### Scenario: Mixed hosted and Git inventory remains complete

- **WHEN** detailed telemetry observes a successful configuration containing Git resources and valid Marketplace resources acquired through root or pathful endpoints using mirror or index-only acquisition
- **THEN** the complete inventory SHALL include both types of resource with separate identity attributes
- **AND** hosted resources SHALL use their canonical coordinate names rather than installed aliases
- **AND** the original acquisition and ownership identities SHALL remain unchanged

#### Scenario: Hosted wire identity survives detailed export

- **WHEN** a valid complete inventory with hosted resources is exported to the primary OTLP endpoint
- **THEN** registry and resource observations SHALL retain source.kind and marketplace.id and the snapshot SHALL share their configuration fingerprint
- **AND** legacy-only observations SHALL omit the new identity attributes
- **AND** basic or disabled telemetry SHALL emit no inventory

#### Scenario: Invalid hosted identity preserves safe omission

- **WHEN** a hosted identity is missing required coordinates, contains malformed or oversized values, or exceeds the inventory cardinality bounds
- **THEN** no partial complete inventory SHALL be emitted and a bounded omission reason SHALL be recorded when detailed telemetry is active
- **AND** a metrics response with an unknown source kind or incoherent hosted identity attributes SHALL retain the existing unavailable-count fallback

### Requirement: Installation refresh health is observable

The operations metrics endpoint SHALL expose refresh attempt outcomes with bounded labels, refresh duration and the last successful evaluation timestamp. Background attempt failures SHALL be logged once per attempt rather than once per affected search. Metrics and logs SHALL NOT disclose Installation IDs, registry signatures, endpoint credentials or backend response payloads.

#### Scenario: Refresh health distinguishes attempts from search fallback

- **WHEN** background refreshes succeed or fail while searches reuse counts or fall back
- **THEN** operations metrics SHALL reflect attempt outcomes, duration and the last successful evaluation timestamp without search fallback creating additional attempt observations
- **AND** failures SHALL produce per-attempt bounded logs without sensitive identifiers or credentials

### Requirement: Filesystem blob containment

A Marketplace filesystem blob store MUST reject addresses that can select a parent directory, an absolute path, or path separators. Blob reads and mutations MUST remain within the configured storage root, including when an address encounters a symlink. Valid content-addressed layouts and immutable publication conflict behavior MUST remain unchanged.

#### Scenario: Blob addresses cannot escape storage

- **WHEN** a downstream consumer reads, writes, checks, or deletes a blob using an unsafe address
- **THEN** the operation MUST fail before accessing or changing external content
- **AND** an existing valid blob MUST remain readable and unchanged

### Requirement: Active support content scheme policy

An inspection projection MUST classify support text beginning with javascript, data, or vbscript URL schemes as metadata-only, ignoring surrounding whitespace and scheme case. Publication MUST reject a projection that attempts to expose such support bytes as textual content. Safe support text MUST remain available.

#### Scenario: Active URL schemes are metadata only

- **WHEN** an inspection projection contains support text beginning with an active URL scheme and ordinary safe support text
- **THEN** active support bytes MUST not be available as preview content
- **AND** their file identity MUST remain available as metadata
- **AND** ordinary safe support bytes MUST remain available

### Requirement: Decode rejections omit unsafe entry names

Decoding a resource artifact MUST reject an archive entry whose name is absolute or contains `..` path components without including that entry name in the returned error. The error MUST still state whether the entry was rejected as an absolute path or as path traversal.

#### Scenario: Unsafe entry name is not echoed

- **WHEN** a `tar.gz` whose entry name is absolute or contains `..` path components is decoded
- **THEN** decoding MUST fail with an error identifying the absolute-path or path-traversal rejection
- **AND** the error MUST NOT contain the entry name

### Requirement: Resource revision source provenance
Resource revision responses for the latest resource, revision lists, and exact revisions SHALL include an optional `source` object with the same shape and sanitization as the catalog `CatalogSource`: a safe HTTP(S) `repository_url`, the normalized repository-relative `path` of the resource in that occurrence, and the exact occurrence `commit`. The response SHALL omit `source` when the occurrence has no recorded source path or source origin, or when the origin is not a safe HTTP(S) URL.

#### Scenario: Resource revision exposes sanitized source provenance
- **WHEN** a client fetches a resource revision whose occurrence recorded a source origin and source path
- **THEN** the response `source` carries the repository URL with userinfo and credential-like query values removed, the normalized path, and the occurrence commit

#### Scenario: Resource revision omits untrustworthy source provenance
- **WHEN** a resource revision has no recorded source path or source origin, or its origin is not a safe HTTP(S) URL
- **THEN** the response omits `source`

### Requirement: Archive digests verify by received bytes

The Marketplace and its clients SHALL verify canonical archive digests by
hashing bytes they hold, without reproducing a compressor's output. A
placeholder-form archive is verified by hashing its bytes against the digest.
A final archive is verified by content equality with the hash-verified
placeholder form. The re-encoding verifier remains only as a fallback for
archives that have no placeholder form.

#### Scenario: Placeholder archive verifies regardless of the encoding toolchain

- **GIVEN** placeholder-form archives recorded by Go 1.26 and by Go 1.27 with
  different digests but identical content
- **WHEN** each is verified by its digest
- **THEN** verification MUST succeed under either toolchain
- **AND** a final archive from the same toolchain MUST verify against its
  placeholder by content equality

#### Scenario: Final archive with different bytes but identical content verifies

- **GIVEN** a verified placeholder-form archive and a final archive whose
  bytes differ from the encoder output but decode to the same content
- **WHEN** the final archive is verified against the placeholder
- **THEN** verification MUST succeed
- **AND** a final archive whose payload, asset, license, or support content
  differs MUST fail verification

#### Scenario: Placeholder archive route serves archive-digest bytes

- **GIVEN** a resource revision owned by the Marketplace with a stored or
  derivable placeholder-form archive
- **WHEN** `GET /api/v1/placeholder-archives/sha256/{hex}` is requested for its archive digest
- **THEN** the response MUST carry bytes whose SHA-256 equals the requested digest
- **AND** the response MUST use the artifact media type
- **AND** an unowned digest MUST return 404, a revoked revision MUST return 410, and a blocked revision MUST return 403

#### Scenario: Missing placeholder archive is derived only when reproducible

- **GIVEN** a stored final archive with no stored placeholder-form archive
- **WHEN** the placeholder route is requested for its archive digest
- **THEN** the Marketplace MUST derive the placeholder archive from the final archive and store it when the derived bytes hash to the digest
- **AND** when the derived bytes do not hash to the digest the response MUST be 404 with a placeholder-archive-unavailable message

#### Scenario: Artifact route serves stored final archives without re-compression

- **GIVEN** a stored final archive recorded by a different Go toolchain whose placeholder-form archive is stored
- **WHEN** the artifact route is requested for its archive digest
- **THEN** the response MUST serve the stored final bytes
- **AND** the response MUST NOT fail with a digest mismatch caused by the server's compressor

#### Scenario: Worker backfills placeholder archives

- **GIVEN** stored resource revisions with archive digests and no placeholder-form archive
- **WHEN** the worker runs the backfill pass or `serve reindex` completes
- **THEN** each reproducible placeholder-form archive MUST be stored under its placeholder key
- **AND** unreproducible archives MUST be counted and reported without failing the pass or readiness

### Requirement: Placeholder archive route is served in the OpenAPI contract

The OpenAPI contract SHALL document `GET /api/v1/placeholder-archives/sha256/{hex}`
with the artifact media type and the same availability responses as the artifact route.

#### Scenario: Placeholder archive route is documented

- **WHEN** the OpenAPI contract is loaded
- **THEN** it MUST define the placeholder-archive route with the artifact media type
- **AND** it MUST document 403, 404, 410, and 500 responses

### Requirement: Clients verify archives by received bytes

A client SHALL verify a resource archive by hashing the placeholder-form archive
it receives from `GET /api/v1/placeholder-archives/sha256/{hex}`, so that locks
and caches written under any Go toolchain verify under any other. A client SHALL
fall back to the final artifact route only when the placeholder route answers
404, and it SHALL treat every other failure from the placeholder route as
terminal. The single remaining re-encoding check applies only to a final archive
without a placeholder form, and its failure SHALL be reported as an encoding
mismatch rather than as tampering, unless the decoded content itself fails
verification.

#### Scenario: Client verifies placeholder archives by hash

- **WHEN** a client resolves a mirrored resource whose archive was published by a different Go toolchain
- **THEN** the client MUST fetch the placeholder-form archive and verify it by hashing its bytes against the locked archive digest
- **AND** the client MUST NOT re-compress the archive to verify it

#### Scenario: Locks written under any toolchain stay valid

- **GIVEN** a lock whose archive digest was recorded by one Go toolchain
- **WHEN** a client built with another Go toolchain resolves, syncs, or checks that lock
- **THEN** the lock MUST verify without changing its archive digest
- **AND** the resource content MUST verify against its content digest

#### Scenario: Fallback to the artifact route only on not found

- **WHEN** the placeholder route answers 404 for an archive digest
- **THEN** the client MUST request the final artifact route for that digest
- **AND** when the placeholder route answers 410, 403, a server error, or a network failure, the client MUST fail with that error without requesting the artifact route

#### Scenario: Unreproducible legacy archive reports an encoding mismatch

- **GIVEN** a Marketplace that does not serve the placeholder form for an archive
- **WHEN** the archive it serves was compressed by an encoder that this build does not reproduce
- **THEN** the client MUST fail with an encoding-mismatch diagnostic that names the resource, the Marketplace, the expected and re-encoded digests, the agent-marketplace Go version, and the remediation to upgrade the Marketplace and run sync
- **AND** a served archive whose decoded content fails verification MUST remain an integrity failure rather than an encoding mismatch

#### Scenario: Cached unreproducible archive is refetched online

- **GIVEN** a cached final archive that this build does not reproduce
- **WHEN** a refresh resolves the resource online
- **THEN** the client MUST discard the cached copy, fetch the archive once, verify the fetched bytes, and cache them in place of the stale copy
- **AND** when the resolution is offline the client MUST fail with the cached-copy diagnostic that asks for `agent-marketplace sync` while online

#### Scenario: Index-only resolution verifies the registry revision manifest by hash

- **WHEN** an index-only resource is resolved from an exact registry occurrence
- **THEN** the client MUST verify that the registry revision manifest payload hashes to the occurrence's manifest digest before using it
- **AND** the locally rebuilt resources MUST match the manifest's kind, name, content digest, and support-file digests
- **AND** a manifest whose payload does not hash to the occurrence digest, or whose resource entries differ from the rebuilt resources, MUST fail resolution

#### Scenario: Received archive bytes verify across toolchains

- **GIVEN** placeholder-form and final archives recorded by Go 1.26 and by Go 1.27
- **WHEN** each placeholder-form archive is verified by hashing its bytes against its digest under either toolchain
- **THEN** verification MUST succeed
- **AND** a final archive recorded by the other toolchain MUST fail with an encoding mismatch, while tampered final content MUST fail as an integrity error

### Requirement: Placeholder-form archives are cached apart from legacy artifacts

A client SHALL cache placeholder-form archives under a location separate from the
legacy artifact path, so that a client built before this change, which validates
whatever it finds at the legacy path, never reads placeholder bytes there. Final-form
archives SHALL be written to the legacy path only when the legacy artifact route was
used. The garbage collector SHALL treat both locations with the same reachability
rules, and SHALL preserve entries it does not recognize.

#### Scenario: Placeholder-form archives stay out of the legacy artifact path

- **WHEN** a client fetches a placeholder-form archive for a locked digest
- **THEN** the client MUST store the placeholder bytes only under the placeholder location
- **AND** the legacy artifact path MUST NOT receive placeholder bytes, so a client that predates this change finds nothing there to validate or refetches

#### Scenario: Cached placeholder-form archive serves offline resolution

- **GIVEN** a verified placeholder-form archive in the cache
- **WHEN** a resolution runs in cached or cached-no-fetch mode
- **THEN** the client MUST resolve the resource from the placeholder location without contacting the Marketplace

#### Scenario: Placeholder-form archive is collected with its archive

- **WHEN** garbage collection runs and a digest is unreferenced
- **THEN** it MUST remove the placeholder-form and legacy entries for that digest together
- **AND** it MUST keep both entries for a referenced digest, and MUST preserve malformed or unrecognized placeholder entries and fail closed on symlinked ones

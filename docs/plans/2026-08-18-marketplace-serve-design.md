# Marketplace serve and discovery

**Status:** Approved design, ready for decomposition

**Date:** 2026-08-18

**Origin:** An approved `/grilling` and `/domain-modeling` session on 2026-08-18

## Promotion boundary

The user approved this design before this document was written. The document authorizes implementation planning and Bead creation. It does not authorize partially enabling a public Marketplace in generated user configuration. The public endpoint is added by `init` only after the public deployment passes the GA gates in this plan.

The active configuration-schema work that keeps v2 canonical must land before Marketplace consumer fields are finalized. Marketplace does not introduce a new major config version solely for this feature.

## Objective

Add a production-capable `agent-marketplace serve` command and a separately deployed web frontend that let public communities and private organizations operate a Marketplace for every Marketplace-selectable resource kind.

A Marketplace:

- indexes approved Git source repositories for discovery;
- optionally mirrors immutable resource artifacts for distribution;
- exposes a versioned HTTP API used by the CLI and web frontend;
- preserves source provenance and immutable revisions;
- is deployable as one small process or split API and worker roles;
- treats indexed content as hostile and never claims that a resource is safe.

## V1 acceptance boundary

V1 is complete only when all of these surfaces support skill, command, subagent, hook, MCP server, and collection resources:

- canonical artifact encode, decode, validation, and digest verification for materialized resource kinds;
- canonical collection-manifest encode, decode, validation, and transactional expansion;
- Marketplace indexing and revision history;
- REST API and OpenAPI contract;
- CLI search, install, update, sync, and status;
- read-only web discovery and inspection;
- index-only and mirror acquisition;
- revocation and security findings;
- all supported harness materialization paths for non-collection resources.

Rules remain outside Marketplace selection. A kind is not shipped early behind partial public behavior.
Collections remain virtual resources and never materialize directly into a harness.

## Non-goals

- Open-ended repository crawling
- Source submissions, approval queues, or admin UI
- Built-in authentication, authorization, tenant isolation, or per-source ACLs
- Artifact or manifest signatures
- Dependency resolution
- Popularity, usage, semantic, or recommendation ranking
- Marketplace discovery in the Bubble Tea TUI
- Helm charts
- Live or agent-executed resource previews
- Cross-backend storage migration tooling
- Built-in Vault, AWS, GCP, or other secret-manager clients
- Multiple tracked refs or release channels per source

Private deployments must use network controls or an authenticating reverse proxy. Public exposure of private-source content without such protection is unsupported.

## Domain model

### Marketplace

One deployment is one policy domain. It has a stable instance ID, canonical endpoint, presentation metadata, approved sources, policy, metadata store, and blob store. The public Marketplace is one deployment; a company runs its own separate deployment.

### Approved source

An approved source represents one Git repository lineage and contains:

- immutable Source ID;
- mutable canonical source slug;
- permanent, non-reassignable prior slug aliases;
- current source origin plus origin history;
- one configured Git ref;
- one or more roots, each with a path and optional source layout;
- index-only or mirror acquisition mode;
- license and publisher metadata;
- optional server-side credential references;
- optional policy and resource-limit overrides.

Operator configuration under the same Source ID is the authority that a moved origin is the same source. Forge-specific immutable repository IDs may be retained as evidence but are not required by the protocol.

Duplicate kind and name pairs across distinct paths in a source do not produce ambiguous coordinates: configured root order deterministically retains the first occurrence, and refresh emits an actionable warning naming both paths so the publisher can assign distinct source-local names. Repeating the same path remains an operator configuration error.

Collection publication expands nested includes, rejects cycles, and resolves every member to an exact resource in the same Marketplace. A member whose existing `registry`, `ref`, or `path` cannot be mapped to an approved source fails that collection's source refresh; publication never leaves a permanently unresolvable collection.

### Resource coordinate and revision

A resource coordinate combines:

1. Marketplace instance ID;
2. Source ID;
3. resource kind;
4. source-local resource name.

Human-facing locators use a Marketplace alias or URL and source slug:

```text
public:mattpocock-skills/skill/grill-me
https://marketplace.example/s/mattpocock-skills/skill/grill-me
```

A resource revision is immutable canonical content and metadata. It contains:

- canonical resource-content digest;
- analyzer version and security findings;
- license and provenance metadata;
- revocation state.

A source occurrence connects one resource revision to an exact source commit, source revision manifest, acquisition mode, and optional mirrored artifact digest. Resource revisions change only when canonical content or metadata changes; source occurrences and source revision history still record every successfully indexed source commit. Artifact blobs deduplicate by digest.

### Installed name

The resource's short source-local name is its default installed name. If any selected Marketplace, Git registry, or local resource already uses that kind and installed name, installation fails and requires `--as <name>`. The CLI never silently prefixes or renames a resource.

### Collection

A collection revision is an immutable manifest of exact member coordinates and revisions. Installation validates and acquires every member before mutating configuration, then writes all pins or none. Collection update explicitly advances the member set.

Declared requirement metadata is preserved for display and validation, but v1 does not resolve it automatically.

## System architecture

```text
                         approved Git sources
                                  |
                                  v
                    +----------------------------+
                    | Marketplace worker         |
                    | clone, ingest, analyze,    |
                    | canonicalize, publish      |
                    +-------------+--------------+
                                  |
                       atomic metadata publication
                                  |
                   +--------------+---------------+
                   |                              |
                   v                              v
          +------------------+          +------------------+
          | Metadata store   |          | Blob store       |
          | SQLite/Postgres  |          | filesystem/S3    |
          +--------+---------+          +--------+---------+
                   |                             |
                   +--------------+--------------+
                                  |
                                  v
                    +----------------------------+
                    | Marketplace API            |
                    | REST, OpenAPI, search,     |
                    | manifests, artifacts       |
                    +-------------+--------------+
                                  |
                  +---------------+----------------+
                  |                                |
                  v                                v
       agent-marketplace CLI              Nuxt web frontend
```

### Process roles

`agent-marketplace serve --config <path>` runs `--role all` by default. Production deployments may run `--role api` and `--role worker` separately.

The API role never clones or ingests a source. The all-in-one topology hosts both roles in one process but preserves the same internal boundary.

`agent-marketplace serve reindex --config <path> [source]` performs a one-shot durable enqueue and waits for the requested indexing work. SQLite uses WAL, a bounded busy timeout, and transaction retry so this transient command can safely coordinate with the one running all-in-one process without becoming a second long-lived writer. V1 exposes no unauthenticated admin mutation endpoint.

### Network defaults

- Default listener: `127.0.0.1:8080`
- Default operations listener: `127.0.0.1:9090`
- Default transport: HTTP
- External bind is explicit
- TLS termination and volumetric DDoS defense belong at the reverse proxy, CDN, or WAF
- Official container examples bind inside the container and publish through a proxy

The application still enforces pagination, body and response limits, deadlines, bounded concurrency, cache headers, and ingestion-free request handling. `/metrics` and detailed readiness are exposed only on the operations listener; the public listener exposes liveness without internal state.

## Marketplace operator configuration

Marketplace server configuration is separate from layered consumer configuration. The exact schema will be specified before implementation; this illustrates the approved shape:

```yaml
apiVersion: agent-marketplace.io/marketplace/v1

instance:
  id: 01JMARKETPLACEEXAMPLE
  name: Example Marketplace
  canonical_url: https://marketplace.example
  # Optional; omit for API-only deployments.
  browser_base: https://marketplace.example

listen: 127.0.0.1:8080

storage:
  metadata:
    driver: sqlite
    dsn: /var/lib/agent-marketplace/marketplace.db
  blobs:
    driver: filesystem
    path: /var/lib/agent-marketplace/blobs

acquisition_default: mirror
refresh:
  interval: 30m

policy:
  hidden_content: reject
  license_allowlist: [MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause]
  limits:
    # Concrete defaults are fixed by benchmarks and adversarial tests.
    repository_bytes: <bounded>
    file_count: <bounded>
    file_bytes: <bounded>
    artifact_bytes: <bounded>
    clone_duration: <bounded>
    ingestion_duration: <bounded>

sources:
  - id: 01JSOURCEEXAMPLE
    slug: example-resources
    aliases: []
    origin: https://github.com/example/resources.git
    ref: main
    acquisition: mirror
    publisher:
      name: Example
    license: MIT
    roots:
      - path: .agents
        source_harness: factory
    credentials:
      ssh_key:
        file: /run/secrets/example-source-key

revocations:
  source_ids: []
  coordinates: []
  revisions: []
```

Secret values are forbidden in server configuration. Credentials may only refer to environment variables or mounted files. Source credentials remain server-side and never enter metadata, artifacts, diagnostics, or logs.

## Storage and job model

### Metadata stores

- SQLite for all-in-one and other explicitly single-writer topologies
- PostgreSQL for multiple API or worker replicas

### Blob stores

- Content-addressed local/shared filesystem
- S3-compatible object storage

Metadata and blob adapters expose capability-oriented interfaces. Backend names and details never appear in the public API. Every supported pairing passes one conformance suite.

SQLite is not presented as horizontally scalable. It supports one long-lived all-in-one process plus transient maintenance commands using WAL, bounded busy waits, and transaction retry. A split process using filesystem blobs requires a shared durable volume; distributed roles use PostgreSQL and normally S3-compatible storage.

Jobs are durable database records with leases, idempotency keys, retry limits, exponential backoff, and jitter. No Redis or external queue is required. Publication uploads and verifies blobs before a metadata transaction makes a revision visible. Orphaned pre-publication blobs are safe to garbage-collect.

The metadata database and blob store are one backup and restore unit. Startup consistency checks fail readiness when a currently published artifact is missing. Explicit repair or reindex operations handle recoverable drift.

## Indexing and publication

Workers reconcile configured sources at startup and on a configured interval.

For each source refresh:

1. Acquire a source-scoped job lease.
2. Resolve the configured ref to an exact commit.
3. Clone or fetch in an isolated ephemeral workspace.
4. Disable Git hooks, submodules, LFS, credential prompts, and unsafe protocol behavior.
5. Enforce clone time, repository size, file count, path, and file-size limits.
6. Ingest every configured root through harness-compilation.
7. Reject root escape, symlinks, device files, duplicate coordinates, and malformed resources.
8. Convert each resource into canonical semantic payload.
9. Run deterministic security analyzers and license policy.
10. Create per-resource artifacts when the source is mirrored.
11. Create the source revision manifest.
12. Upload and verify immutable blobs.
13. Atomically publish the complete source revision.

No source becomes partially visible. A failed refresh retains the last known-good revision and marks the source stale or failed. Limit failures never truncate a resource.

Removing a source from configuration unlists it but permanently reserves its Source ID and slugs. Unlisted sources and resources disappear from search and browse results, while immutable detail and resolution routes remain available for existing pins until explicit retention garbage collection.

Revoked items remain visible as provenance records but cannot be resolved for ordinary installation, update, or online synchronization. Revocation is a control-plane guarantee: it cannot recall bytes already stored by clients or intermediary caches. Online operations refresh revocation state before use. Offline sync enforces the last cached revocation state and emits a warning that newer revocations cannot be observed.

## Canonical resource artifact

The artifact is a deterministic tar.gz identified by SHA-256 over the exact archive bytes. Artifact schema v1 fixes the gzip header fields, compression level, DEFLATE profile, tar ordering, and tar metadata. Golden byte fixtures pin the encoding across supported platforms and toolchain upgrades. An encoder change that cannot reproduce v1 bytes requires a new artifact schema version; it must not silently change digests for unchanged v1 content. The archive contains:

```text
manifest.json
resource/<canonical payload>
assets/<contained support files>
licenses/<applicable license and notice files>
```

The versioned manifest records kind, name, Source ID, resource revision, canonical content digest, supported schema version, capability metadata, security findings, license, and source identity provenance. Commit-specific provenance and the artifact digest are stored outside the archive in the source revision manifest and metadata store, avoiding both self-reference and duplicate artifacts for unchanged content across commits.

The payload preserves resource semantics and supporting files, not source-layout accidents. Original source paths are provenance metadata only. Clients decode the canonical form and compile it into selected harness targets.

Archive construction and extraction forbid:

- absolute or parent-relative paths;
- symlinks and hard links;
- device files, sockets, and named pipes;
- duplicate or case-colliding paths;
- files or expanded totals above configured limits;
- non-canonical timestamps, owners, modes, and ordering.

Manifest and payload canonicalization must be byte-deterministic across supported platforms.

## HTTP API

The API is versioned JSON over HTTP and described by OpenAPI. Additive changes remain within `/api/v1`; breaking changes use a new major. Capabilities make version support explicit.

Minimum routes:

```text
GET /.well-known/agent-marketplace.json
GET /api/v1/capabilities
GET /api/v1/instance
GET /api/v1/search
GET /api/v1/sources/{source}
GET /api/v1/sources/{source}/history
GET /api/v1/resources/{source}/{kind}/{name}
GET /api/v1/resources/{source}/{kind}/{name}/revisions
GET /api/v1/resources/{source}/{kind}/{name}/revisions/{revision}
GET /api/v1/manifests/{source-revision}
GET /api/v1/artifacts/sha256/{digest}
GET /healthz

# operations listener only
GET /readyz
GET /metrics
```

Capabilities declare:

- API major versions;
- artifact schema versions;
- supported resource kinds;
- index-only and mirror support;
- optional server features.

Artifact routes use immutable cache headers and download content types. Search returns the latest non-revoked revision per coordinate from currently listed sources; detail routes expose immutable history, stale state, revocation, provenance, and findings. Revocation blocks resolution but cannot invalidate previously cached immutable artifact bytes.

The stable browser resource route is:

```text
/s/{source}/{kind}/{name}
```

The well-known document maps that URL back to the API base and instance ID for CLI installation.

## Consumer configuration and CLI

The final field syntax depends on the canonical config-schema work. The approved semantic shape is a top-level `marketplaces:` list parallel to `registries:`:

```yaml
marketplaces:
  - url: https://marketplace.example
    instance_id: 01JMARKETPLACEEXAMPLE
    alias: public
    resources:
      - source_id: 01JSOURCEEXAMPLE
        source_slug: example-resources
        kind: skill
        name: review
        revision: <immutable revision>
        digest: sha256:<digest>
        acquisition: mirror
        installed_name: review
```

Aliases are machine-local conveniences. Installation resolves aliases before writing committed configuration, which stores the canonical URL and instance ID.

### Search

```text
agent-marketplace search [query]
agent-marketplace search [query] --marketplace <alias|url|all>
```

Bare search queries all configured Marketplaces using bounded parallelism and per-endpoint timeouts. Results always identify their Marketplace. The CLI trusts each configured server's returned relevance score and uses only deterministic tie-breaking; it does not attempt to normalize independent ranking models.

Partial multi-endpoint failure returns available results plus named warnings. JSON output includes structured per-endpoint errors. A sole requested endpoint failure returns nonzero.

Search queries are not retained in application logs or metric labels by default.

### Install

```text
agent-marketplace install public:example-resources/skill/review
agent-marketplace install https://marketplace.example/s/example-resources/skill/review
agent-marketplace install public:other/skill/review --as other-review
```

Before mutation, install shows:

- Marketplace and source publisher/operator distinction;
- current source origin and origin history;
- exact revision and digest;
- index-only or mirror acquisition;
- resource kind and declared tools/capabilities;
- structural and heuristic security findings;
- revocation and license state.

Interactive installs require confirmation when findings or active capabilities exist. Automation passes an explicit acceptance flag. Exact organization-managed required pins remain inspectable.

Every effective-config load validates Marketplace endpoints against `allowed_marketplaces`, including sync, status, update, search, and installs from committed or previously written configuration. Index-only acquisition also validates the resolved Git origin against `allowed_registries` before every fetch. Mirror acquisition validates the Marketplace endpoint. A trusted Marketplace never bypasses origin policy for client-side Git access.

### Sync, status, update, and cache

- `sync` never advances Marketplace revisions.
- `status` reports stale, revoked, unavailable, and update-available state.
- `update` explicitly resolves and writes a newer exact revision.
- Cached artifact blobs are keyed by digest, separated from Git checkouts, and verified before every materialization.
- Index-only content is re-ingested into canonical form and compared with the pinned canonical content digest before materialization.
- `sync` works offline when every exact pin is cached.
- `gc` removes unreferenced artifact blobs under cache policy.
- A revoked pin is a hard error when current or cached revocation state marks it revoked.
- `sync --allow-revoked <coordinate@digest>` is exact, one-time, and non-persistent; status remains unhealthy.

Index-only pins retain the last known origin as provenance. On cache miss after a source move, the client asks the original Marketplace to resolve the permitted current origin of the exact revision, then applies `allowed_registries`. It never falls back to another Marketplace.

Organization config may require exact resource or collection pins. Whole Marketplaces and sources cannot be required or default-selected.

## Security model

The complete STRIDE analysis is in `.factory/threat-model.md`.

### Trust semantics

Source approval means the operator permits indexing and distribution. It is not a safety certification or endorsement. The UI and manifests distinguish source publisher from Marketplace operator.

The Marketplace never executes, imports into an agent, or dynamically evaluates a resource. Preview is sanitized or escaped static content only.

### Content findings

V1 deterministic analyzers cover:

- HTML comments and zero-width characters;
- terminal controls and unsafe bidirectional controls;
- dangerous URL schemes and remote references;
- executable or suspicious assets;
- declared tools, permissions, hooks, and MCP capabilities;
- suspicious paths and archive structure;
- common encoded or obfuscated instruction patterns.

Terminal controls and unsafe bidirectional controls are rejected. HTML comments and zero-width characters produce high-visibility findings; public policy rejects them, while private operators may configure another outcome. The system never silently rewrites prompt semantics.

Heuristic prompt-injection patterns are warnings, not proof. Each finding stores analyzer version, stable code, severity, policy outcome, and source range where available. The Marketplace never displays a generic “safe” badge.

### Browser rendering

- Raw HTML in Markdown is disabled.
- Rendered links use an allowlisted scheme and safe external-link attributes.
- Remote images are disabled by default and never fetched server-side.
- Raw content is shown only in an escaped text view with findings highlighted.
- Content Security Policy forbids inline script and unapproved origins.
- Catalog text is treated as untrusted in logs and terminals.

## Web frontend

The `web/` application:

- uses Nuxt 4 and Nuxt UI core;
- may copy a small number of reviewed Inspira UI components;
- uses no private Bento runtime or build dependency;
- generates its TypeScript API client from the Go-owned OpenAPI contract;
- is built and released as a separate OCI image and artifact;
- accepts runtime API base and constrained theme metadata;
- prefers same-origin deployment behind a reverse proxy;
- supports strict configured CORS only for development or intentional split-domain use.

V1 pages include:

- search and filters;
- resource detail and revision history;
- source pages and origin history;
- provenance, digest, license, stale, and revocation state;
- capability and security-finding summaries;
- sanitized Markdown preview;
- escaped raw-content inspection;
- copyable install commands.

Login, submissions, ratings, comments, admin controls, and arbitrary operator HTML or JavaScript are excluded.

Frontend acceptance requires WCAG 2.2 AA, complete keyboard navigation, reduced-motion support, responsive layouts, and measured performance budgets. Inspira effects must degrade cleanly and cannot regress core browsing.

## Observability

V1 includes:

- process-wide structured logs;
- liveness and readiness;
- Prometheus metrics;
- source and revision identifiers where cardinality is bounded;
- refresh duration, outcome, queue age, API duration, response class, and storage health.

Logs and labels exclude repository credentials, secret values, artifact content, raw search queries, and client identity. Usage telemetry is a separate future decision.

## Implementation phases

### Phase 0: prerequisites and contracts

1. Resolve the canonical config-schema bead.
2. Add an OpenSpec change for Marketplace behavior and client configuration.
3. Freeze domain types, artifact schema v1, OpenAPI v1, finding codes, and conformance fixtures.
4. Add benchmarks before modifying catalog discovery, sync, status, or other user-visible hot paths.

### Phase 1: canonical artifact and analyzer library

1. Extend harness-compilation with stable canonical resource serialization for every selectable kind.
2. Define deterministic tar.gz creation and safe extraction.
3. Implement source revision manifests and SHA-256 verification.
4. Implement structural analyzers and finding contracts.
5. Add round-trip, cross-platform determinism, malicious archive, hidden-content, and all-kind matrix tests.

### Phase 2: Marketplace domain and persistence

1. Implement Marketplace domain entities and state transitions.
2. Add metadata-store and blob-store interfaces.
3. Implement SQLite, PostgreSQL, filesystem, and S3-compatible adapters.
4. Add schema migration ownership, conformance suites, job leasing, retention, revocation, and consistency checks.

### Phase 3: indexing worker

1. Parse and validate separate Marketplace config.
2. Implement bounded isolated Git acquisition.
3. Reuse harness-compilation ingestion for multiple configured roots.
4. Canonicalize, analyze, license-check, and atomically publish revisions.
5. Add scheduler, backoff, one-shot reindex, stale-state, and source move/history handling.

### Phase 4: API and serve command

1. Add `serve`, role separation, safe listener defaults, and graceful shutdown.
2. Implement REST/OpenAPI routes, capabilities, search, provenance, manifests, and artifact delivery.
3. Add pagination, deadlines, request limits, cache behavior, CORS, health, readiness, and metrics.
4. Add API compatibility and hostile-input tests.

### Phase 5: CLI consumer integration

1. Finalize Marketplace fields in the canonical layered config schema.
2. Add Marketplace endpoint policy and merge semantics.
3. Implement federated `search`.
4. Implement locator resolution, risk consent, exact pinning, aliases, and transactional collection install.
5. Extend sync, status, update, cache, GC, offline behavior, revocation, and managed required pins.
6. Add unit, integration, and isolated process E2E coverage.

### Phase 6: web frontend

1. Scaffold Nuxt 4 and Nuxt UI core under `web/`.
2. Generate the TypeScript client from OpenAPI in CI.
3. Implement search, source, resource, revision, findings, raw inspection, and install-copy flows.
4. Add safe Markdown rendering, CSP, constrained theming, accessibility, responsive, and performance tests.
5. Add selected Inspira effects only after core budgets pass.

### Phase 7: packaging and GA

1. Ship API/worker and frontend OCI images.
2. Add Docker Compose examples for SQLite/filesystem and PostgreSQL/S3-compatible topologies.
3. Publish operator, backup/restore, reverse-proxy, security, and incident guidance.
4. Run all-kind conformance, storage matrix, E2E, security, performance, and frontend gates.
5. Deploy the public Marketplace with curated source and license policy.
6. Set the launch-configured public URL and only then add it to `init` with explicit opt-out.

## Validation strategy

### Unit and property tests

- Stable identities, slugs, aliases, moves, and rename migrations
- Deterministic canonicalization and archive bytes
- Digest and manifest verification
- Path, symlink, case-collision, archive, and decompression attacks
- Analyzer codes and source ranges
- Config validation, policy precedence, and exact revocation override
- Store and job state machines

### Conformance matrices

- Every materialized resource kind through canonical artifact encode/decode
- Collection manifests through canonical encode/decode and transactional expansion
- Every non-collection resource kind through each supported harness target
- SQLite/PostgreSQL metadata behavior
- Filesystem/S3 blob behavior
- API and artifact schema version negotiation

### Integration and E2E

- Source refresh success, failure, stale retention, move, unlist, retention, and revocation
- Index-only and mirror install
- Allowed Marketplace and Git-origin policy
- Multi-Marketplace partial search failures
- Name collisions and `--as`
- Transactional collections
- Offline sync and missing cache
- Separate API/worker topology
- Docker Compose smoke tests

### Frontend

- Generated-client drift check
- Component and route behavior
- Sanitized Markdown and escaped raw content
- CSP and hostile content fixtures
- Keyboard and screen-reader semantics
- WCAG automated checks plus manual review
- Browser E2E against the real API
- Performance budgets on search and resource detail pages

### Performance

Marketplace work touches catalog discovery, sync, status, artifact loading, search, and HTTP paths. Benchmarks must be added and baselined before each hot path is changed, then compared after implementation according to repository policy.

## Primary risks and mitigations

| Risk | Mitigation |
|---|---|
| Malicious or compromised source | Isolated bounded ingestion, structural analyzers, visible findings, policy gates, static preview, revocation |
| Prompt injection hidden from humans | Reject controls, flag hidden carriers, public reject policy, escaped raw inspection, no safety claims |
| Artifact tampering | Immutable revisions, SHA-256 verification, atomic publication, HTTPS; signatures reserved for later |
| Browser XSS or tracking | Strict Markdown, URL allowlist, no remote images, escaped raw text, CSP |
| Private content disclosure | Server-side source credentials, no secret persistence, external network/auth protection requirement |
| DDoS or resource exhaustion | Bounded requests and workers, hard source limits, backoff, no ingestion on request path, operator edge defense |
| License violation | License detection/assertion, allowlist policy, notices in artifacts, index-only or reject fallback |
| Repository moves and takeovers | Immutable Source ID, origin history, permanent slug aliases, operator continuity assertion |
| Storage split-brain or missing blobs | Topology constraints, leases, upload-before-publish, consistency checks, DB/blob joint backups |
| Public config shipped before service is ready | Public endpoint omitted from init until GA deployment passes gates |

## Launch inputs, not architecture gaps

These values are intentionally chosen during public deployment preparation:

- canonical public site/API URL;
- public Marketplace instance ID and presentation;
- initial curated sources and publisher metadata;
- concrete public license allowlist;
- concrete size, duration, and concurrency limits based on benchmarks;
- frontend performance budgets;
- public deployment owner, backup policy, and incident contacts.

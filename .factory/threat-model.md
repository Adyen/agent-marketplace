# Threat Model for Agent Marketplace

**Last Updated:** 2026-08-18
**Version:** 1.0.0
**Methodology:** STRIDE + natural-language analysis
**Scope:** Existing CLI plus the approved, not-yet-implemented Marketplace serve architecture

---

## 1. System Overview

### Architecture description

Agent Marketplace is a Go CLI that ingests agent resources from Git or local source repositories, converts them into a harness-neutral intermediate representation, resolves configured selections, and materializes them into agent harness layouts. The approved Marketplace extension adds a public HTTP API, an asynchronous ingestion worker, durable metadata and artifact stores, canonical resource artifacts, and a separate Nuxt frontend.

The security-critical components are:

1. **CLI and layered configuration**: accepts registry and Marketplace endpoints, selects exact resources, enforces organization policy, caches content, and writes harness files.
2. **Git acquisition and harness-compilation ingestion**: reads untrusted repository trees and converts skills, commands, subagents, hooks, MCP servers, and collections into canonical resource semantics.
3. **Marketplace worker**: resolves configured source refs, clones in bounded workspaces, analyzes content, creates deterministic artifacts, and atomically publishes complete revisions.
4. **Marketplace API**: exposes capabilities, search, provenance, manifests, immutable artifacts, health, readiness, and metrics.
5. **Metadata and blob storage**: stores source history, immutable revision metadata, security findings, revocations, jobs, and artifact bytes.
6. **Nuxt frontend**: renders untrusted catalog metadata and Markdown as a read-only discovery interface.
7. **Harness materialization**: compiles canonical resources into target-specific files, symlinks, and shared configuration aggregates that agent runtimes may execute or interpret.

### Key components

| Component | Purpose | Security criticality | Attack surface |
|---|---|---:|---|
| CLI config loader | Resolve policy and exact pins | HIGH | YAML files, flags, environment |
| Git acquisition | Fetch source repositories | CRITICAL | Source URLs, refs, Git protocols, repository contents |
| Harness ingestion | Parse heterogeneous layouts | CRITICAL | Markdown, YAML, JSON, files, paths, symlinks |
| Marketplace worker | Analyze and publish artifacts | CRITICAL | Source config, credentials, jobs, archives |
| Metadata store | Durable catalog and job state | HIGH | SQL queries, migrations, concurrent workers |
| Blob store | Immutable artifact storage | HIGH | Object keys, uploads, downloads, consistency |
| Marketplace API | Public read surface | HIGH | HTTP paths, queries, pagination, artifact downloads |
| Nuxt frontend | Human discovery and inspection | HIGH | Untrusted text, Markdown, links, raw views |
| Materializer | Write agent runtime configuration | CRITICAL | Artifact extraction, target paths, shared files |

### Data flow

An operator declares an approved Git source in separate Marketplace configuration. A worker resolves the configured ref to a commit, fetches the repository into an isolated workspace, ingests configured roots through harness-compilation, runs structural security and license checks, builds deterministic canonical artifacts, uploads blobs, and commits a complete source revision in one metadata transaction.

A client searches one or more explicitly configured Marketplace endpoints. Installation resolves a human locator to a stable instance, source, coordinate, revision, and digest. Mirror mode downloads and verifies an artifact; index-only mode resolves the exact Git origin and still applies client origin policy. The client caches exact content and materializes it into selected harness targets. At no point does the Marketplace execute indexed resource instructions.

---

## 2. Trust Boundaries & Security Zones

### Trust zones

1. **Untrusted external zone**
   - Anonymous API clients
   - Public web browsers
   - Git servers and source repositories
   - Markdown, metadata, assets, archives, and search input

2. **Operator-controlled edge zone**
   - TLS termination
   - Optional authentication proxy and network policy
   - CDN, WAF, and volumetric rate limiting
   - Private Marketplace access controls

3. **Marketplace service zone**
   - API process
   - Scheduler and ingestion workers
   - Ephemeral clone workspaces
   - Server configuration and secret references

4. **Durable data zone**
   - SQLite or PostgreSQL metadata
   - Filesystem or S3-compatible artifact blobs
   - Database migrations, backups, and restore operations

5. **Client machine zone**
   - Layered configuration
   - Organization allowlists
   - Git and artifact caches
   - Harness target directories
   - Agent runtime that consumes materialized content

### Authentication and authorization

Marketplace v1 deliberately implements no built-in identity or per-resource authorization. Public deployments are anonymous. Private deployments must be protected by operator network controls or an authenticating reverse proxy. One deployment represents one policy domain.

This is an accepted v1 boundary, not an assertion that private content is safe on an exposed listener. The server binds to localhost by default. Source credentials are server-side environment or file references and never become client credentials.

**Critical security controls:**

- `allowed_marketplaces` policy for every Marketplace endpoint
- `allowed_registries` policy for Git origins used by index-only acquisition
- Exact revision and SHA-256 pinning
- No silent acquisition fallback
- Bounded isolated ingestion and no ingestion on API request paths
- Static rendering and no resource execution
- External protection required for private deployments

---

## 3. Attack Surface Inventory

### Existing interfaces

- CLI flags and arguments in `cmd/agent-marketplace/`
- Layered YAML configuration in `internal/config/`
- Git subprocess execution in `internal/cache/registry.go`
- Repository path and filesystem ingestion in `internal/catalog/`
- Markdown, YAML, JSON, JSONC, and target-specific parsers in `pkg/harness-compilation/`
- Materialization paths and shared aggregate files in `internal/materialize/` and `internal/ownership/`

### Planned public HTTP endpoints

- `GET /.well-known/agent-marketplace.json`
  - Input: none
  - Validation: fixed response
  - Risk: endpoint spoofing if transport or configured identity is not trusted

- `GET /api/v1/capabilities`
  - Input: none
  - Validation: fixed versioned response
  - Risk: compatibility downgrade or false capability advertisement by an impersonated endpoint

- `GET /api/v1/instance`
  - Input: none
  - Validation: fixed response containing the stable instance ID and safe presentation metadata
  - Risk: instance-identity confusion if the client does not compare the configured ID

- `GET /api/v1/search`
  - Input: query, kind, source, tags, sorting, pagination
  - Validation: bounded lengths and page sizes, enumerated filters
  - Risk: CPU/database exhaustion, injection, query privacy leakage

- `GET /api/v1/sources/...` and `GET /api/v1/resources/...`
  - Input: source slug or ID, kind, name, revision
  - Validation: canonical grammar, parameterized lookups
  - Risk: object confusion, unbounded history, hostile content rendering

- `GET /api/v1/manifests/...`
  - Input: immutable source revision
  - Validation: canonical revision identifier
  - Risk: provenance tampering or stale/revoked state omission

- `GET /api/v1/artifacts/sha256/{digest}`
  - Input: exact SHA-256 digest
  - Validation: fixed digest grammar and metadata authorization state
  - Risk: bandwidth exhaustion, missing revocation checks, content-type confusion

- Public `GET /healthz`
  - Input: none
  - Validation: fixed minimal liveness response
  - Risk: low-cost request flooding

- Operations-listener `GET /readyz`, `GET /metrics`
  - Input: none
  - Validation: fixed routes on a localhost-bound, separately controlled listener
  - Risk: internal topology or high-cardinality data disclosure if an operator exposes the listener

There are no v1 file-upload or admin mutation endpoints.

### Other input vectors

1. Marketplace server YAML and environment/file secret references
2. Git URLs, refs, roots, redirects, repository contents, and forge behavior
3. Artifact archives and manifests
4. SQL data and storage object listings
5. Frontend runtime API and theme configuration
6. Operator reverse-proxy headers

---

## 4. Critical Assets & Data Classification

### PII

V1 does not require user accounts or retain client identity. Network infrastructure may observe client IPs outside the application. Raw search queries are intentionally excluded from logs and metric labels.

### Credentials and secrets

- Private Git SSH keys, tokens, or credential files
- PostgreSQL credentials
- S3-compatible access credentials
- Reverse-proxy or edge credentials

Protection measures:

- Refer to source credentials through environment variables or mounted files
- Never serialize credentials into metadata, artifacts, logs, findings, or diagnostics
- Redact credential-bearing URLs
- The planned Marketplace worker passes a minimal environment to Git subprocesses and disables dangerous Git protocols
- Scope source credentials to the worker, not the API or frontend

The existing CLI Git path in `internal/cache/registry.go` inherits the process environment and does not yet enforce the planned URL, option-separator, or protocol restrictions. This is a live gap tracked by `agent-marketplace-pf99.9`, not an existing mitigation, and must be fixed independently of `serve`.

### Business-critical data

- Marketplace instance and Source IDs
- Approved source configuration and origin history
- Immutable manifests, artifact digests, and artifact bytes
- Security findings and revocation records
- Job leases and publication state
- Consumer exact pins and organization policy
- Harness output and aggregate ownership state

Loss or tampering can break reproducibility, distribute malicious instructions, or cause agent runtimes to execute unintended behavior.

---

## 5. Threat Analysis (STRIDE)

### S: Spoofing identity

#### Threat S1: Malicious server impersonates a configured Marketplace

**Scenario:** An attacker redirects DNS or intercepts plain HTTP, returns a different instance ID and resource manifest, and convinces a client to install attacker content.

**Vulnerable components:** CLI Marketplace client, well-known discovery, reverse proxy.

**Attack path:**

1. User configures or resolves a Marketplace URL.
2. Attacker controls transport or endpoint routing.
3. Attacker returns a valid-looking resource and digest.
4. Client pins and materializes attacker content.

**Existing/planned mitigations:**

- Production endpoints use HTTPS through operator edge infrastructure.
- Configuration persists canonical URL plus stable instance ID.
- A changed instance ID is a hard identity mismatch, not transparent replacement.
- Artifacts are pinned and verified by digest.

**Gap:** Signatures and independent key distribution are deferred.

**Severity:** HIGH | **Likelihood:** MEDIUM

#### Threat S2: Repository takeover masquerades as a moved source

**Scenario:** An old repository name is reassigned or an attacker submits a look-alike origin.

**Mitigations:**

- Immutable Source IDs
- Permanent, non-reassignable source-slug aliases
- Operator assertion for origin changes
- Displayed origin history and publisher/operator distinction

**Severity:** HIGH | **Likelihood:** MEDIUM

### T: Tampering with data

#### Threat T1: Source content escapes its root or alters publication state

**Scenario:** A repository contains parent traversal, symlinks, case collisions, malformed archives, submodules, or Git behaviors that cause the worker to read secrets or publish unintended files.

**Vulnerable components:** Git acquisition, root discovery, artifact construction.

**Mitigations:**

- Ephemeral bounded workspace
- No Git hooks, submodules, LFS, or interactive credentials
- Lexical and resolved path containment
- Symlink, hard-link, device, socket, pipe, absolute-path, parent-path, and case-collision rejection
- Atomic all-or-nothing source publication

**Severity:** CRITICAL | **Likelihood:** HIGH

#### Threat T2: Artifact or manifest bytes are replaced

**Scenario:** Storage corruption or an attacker with blob access replaces bytes at an existing digest key.

**Mitigations:**

- Content-addressed keys
- SHA-256 verification after upload and on every client materialization
- Upload-before-metadata publication
- Immutable cache headers for content-addressed bytes, with revocation enforced at resolution and online synchronization
- Startup and readiness consistency checks

**Gaps:**

- A fully compromised Marketplace can publish a new malicious revision because signatures are not independent from the operator in v1.
- Revocation cannot recall artifact bytes already stored in a client or intermediary cache. Offline clients can enforce only their last cached revocation state.

**Severity:** CRITICAL | **Likelihood:** MEDIUM

#### Threat T3: SQL or object-key injection changes catalog state

**Code pattern to reject:**

```go
// Vulnerable
query := "SELECT * FROM resources WHERE source_id = '" + sourceID + "'"

// Safe
row := db.QueryRowContext(ctx,
    "SELECT ... FROM resources WHERE source_id = $1", sourceID)
```

Storage object keys are constructed only from validated fixed identifiers and digests, never raw URL or path input.

**Severity:** HIGH | **Likelihood:** MEDIUM

#### Threat T4: Materialization escapes target roots or corrupts handwritten state

**Scenario:** A resource-controlled name, artifact path, symlink parent, or corrupt ownership record causes synchronization to write outside a harness root, overwrite an unmanaged shared entry, or prune a handwritten hook or MCP server.

**Vulnerable components:** Canonical artifact extraction, `internal/materialize`, shared aggregate reconciliation, and `internal/ownership`.

**Existing mitigations:**

- Resource-name and output-path validation
- Parent-symlink and root-containment checks before portable output writes
- Atomic file replacement and mandatory backups for shared aggregates
- Durable external ownership evidence
- Fail-closed behavior when ownership state is absent or corrupt
- Per-file or per-component prune suppression on partial failures

**Gaps:**

- Marketplace artifact extraction is planned and must preserve the same or stronger containment contract.
- Every new harness compiler and aggregate writer requires adversarial target-root and ownership tests.

**Severity:** CRITICAL | **Likelihood:** MEDIUM

### R: Repudiation

#### Threat R1: Operator cannot explain why a source, move, or revision was published

**Scenario:** A malicious or mistaken configuration change updates an origin or revocation, but no durable evidence links the resulting state.

**Mitigations:**

- Origin history under immutable Source ID
- Source revision manifests with commit, policy, analyzer version, and findings
- Structured worker outcome logs without secret content
- Durable job and publication state

**Gap:** Config-author identity and approval workflow are external in v1 because server configuration is file-administered.

**Severity:** MEDIUM | **Likelihood:** MEDIUM

### I: Information disclosure

#### Threat I1: Private source credentials or content leak

**Scenario:** Credentials appear in clone errors, metadata, artifacts, logs, frontend responses, or API process environment. A private Marketplace is exposed without external protection.

**Mitigations:**

- File/environment secret references only
- Minimal worker-only credential scope
- URL and diagnostic redaction
- No secrets in manifests or findings
- Localhost bind by default
- Explicit external-protection requirement

**Accepted risk:** V1 has no built-in read authorization. An operator who exposes private content anonymously has violated the deployment contract.

**Severity:** CRITICAL | **Likelihood:** MEDIUM

#### Threat I2: Remote Markdown content tracks viewers

**Scenario:** A resource embeds a remote image or URL that leaks viewer IP, referrer, or query data when rendered.

**Mitigations:**

- Remote images disabled by default
- No server-side remote fetches for rendering
- Strict link schemes and safe external-link attributes
- Restrictive Content Security Policy

**Severity:** MEDIUM | **Likelihood:** HIGH

#### Threat I3: Metrics or logs expose searches and high-cardinality identifiers

**Mitigations:**

- No raw query text or client identity in application logs
- No queries, URLs, or unbounded resource names in metric labels
- Bounded internal identifiers only where operationally necessary

**Severity:** MEDIUM | **Likelihood:** MEDIUM

### D: Denial of service

#### Threat D1: Malicious source exhausts clone, parse, compression, or storage resources

**Scenario:** An approved or compromised source contains huge history, too many files, compression bombs, pathological Markdown/YAML, or frequent moving refs.

**Mitigations:**

- Configurable repository, file-count, file-size, artifact, clone-duration, and ingestion-duration limits
- Bounded worker concurrency and durable leases
- Per-source retry backoff and jitter
- Expanded archive size and entry-count limits
- Last known-good state retained on failure

**Severity:** HIGH | **Likelihood:** HIGH

#### Threat D2: Public clients exhaust API or artifact bandwidth

**Mitigations:**

- Pagination and maximum page size
- Query-length limits and database indexes
- Request deadlines and bounded concurrency
- Immutable artifact caching and range policy specified before implementation
- No ingestion in request paths
- CDN/WAF/global rate limits at operator edge

**Accepted risk:** `serve` does not attempt volumetric DDoS defense.

**Severity:** HIGH | **Likelihood:** HIGH

#### Threat D3: Federated CLI search hangs on one endpoint

**Mitigations:**

- Bounded parallel fan-out
- Per-endpoint deadlines
- Partial results with named structured errors

**Severity:** LOW | **Likelihood:** HIGH

### E: Elevation of privilege

#### Threat E1: Prompt injection induces privileged agent actions

**Scenario:** A skill, command, or subagent body includes visible or hidden instructions to read credentials, invoke shell tools, alter policy, or exfiltrate data. HTML comments or zero-width text hide the instructions from human preview.

**Vulnerable components:** Resource content, browser preview, install consent, downstream agent runtime.

**Mitigations:**

- Findings for comments, zero-width content, remote references, executables, tools, permissions, hooks, and MCP capabilities
- Rejection of terminal controls and unsafe bidirectional controls
- Public policy rejects hidden-content findings
- Escaped raw view with source ranges
- Risk summary and explicit install acceptance
- No “safe” certification
- Revocation after discovery

**Gap:** Visible semantic prompt injection cannot be reliably classified. Publisher and user trust remain necessary.

**Severity:** CRITICAL | **Likelihood:** HIGH

#### Threat E2: Browser XSS executes with Marketplace origin privileges

**Code pattern to reject:**

```ts
// Vulnerable
element.innerHTML = resource.body

// Safe direction
const rendered = renderMarkdown(resource.body, {
  html: false,
  allowedSchemes: ['https', 'http'],
  images: false
})
```

**Mitigations:**

- Raw HTML disabled
- Sanitized Markdown allowlist
- Escaped raw text
- No MDX or dynamic evaluation
- Restrictive CSP
- Safe URL schemes

**Severity:** HIGH | **Likelihood:** HIGH

#### Threat E3: Command injection through Git or subprocess arguments

**Code pattern to reject:**

```go
// Vulnerable
exec.Command("sh", "-c", "git clone "+sourceURL)

// Safe direction
exec.CommandContext(ctx, "git",
    "-c", "protocol.ext.allow=never",
    "clone", "--", sourceURL, destination)
```

The planned worker validates URL schemes, rejects option-like URLs and refs, separates every argument, disables dangerous Git protocols, sets `GIT_TERMINAL_PROMPT=0`, and uses an allowlisted environment. The existing CLI does not yet satisfy this contract and remains a tracked gap.

**Severity:** CRITICAL | **Likelihood:** MEDIUM

---

## 6. Vulnerability Pattern Library

### SQL injection

Look for string concatenation, `fmt.Sprintf`, or dynamic identifiers in SQL. Use parameterized statements and fixed query templates. Dynamic sort fields map through an enum-to-SQL whitelist.

### XSS and unsafe Markdown

Look for `v-html`, `innerHTML`, MDX execution, raw HTML Markdown options, unsafe `javascript:` or `data:` schemes, remote images, and unsanitized SVG. Use Vue text interpolation, strict Markdown configuration, URL allowlists, escaped raw views, and CSP.

### Command injection

Look for `sh -c`, `bash -c`, interpolated Git arguments, inherited broad environments, and user-controlled executable paths. Use `exec.CommandContext` with separate arguments, fixed executable names, minimal environment, timeouts, and bounded output.

### Path traversal and archive extraction

```go
// Vulnerable
target := filepath.Join(root, header.Name)
_ = os.WriteFile(target, data, 0o644)

// Safe direction
clean := path.Clean(header.Name)
if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
    return ErrArchivePath
}
target := filepath.Join(root, filepath.FromSlash(clean))
if !isWithin(root, target) {
    return ErrArchivePath
}
```

Also reject links, special files, duplicate paths, case-fold collisions, excessive entries, and excessive expanded bytes.

### Authentication bypass

V1 has no application authentication. Look for code that accidentally treats proxy headers such as `X-User` as authorization. Either ignore them or introduce an explicitly designed authenticated boundary in a later version.

### IDOR and object confusion

Every resource lookup is scoped by Marketplace instance, Source ID, kind, name, and revision. Slugs are aliases, not primary keys. Never resolve a digest or revision from another instance or source merely because text matches.

### Prompt injection and hidden content

Look for HTML comments, zero-width characters, bidirectional controls, terminal escapes, remote URLs, encoded instruction blocks, executable assets, broad tool grants, hooks, and MCP commands. Structural analyzers produce stable evidence; heuristic text matches warn but never assert safety or malice.

---

## 7. Security Testing Strategy

| Test class | Purpose | Frequency |
|---|---|---|
| Go unit/property tests | Parsers, paths, archives, canonicalization, policy | Every commit |
| Go fuzz tests | Markdown/frontmatter, manifests, archives, API identifiers | Scheduled and before GA |
| Store conformance | SQL isolation, leases, atomic publication | Every adapter change |
| API tests | Limits, pagination, content types, revocation | Every API change |
| Browser security tests | XSS, CSP, unsafe links, escaped raw view | Every frontend change |
| Dependency and secret scanning | Vulnerable packages and leaked credentials | Every commit |
| Container scanning | API and frontend image dependencies | Every release |
| Adversarial E2E | Hostile repositories and partial failures | Before GA and security releases |

Manual security review is required for:

- canonical artifact or manifest changes;
- Git subprocess and credential handling;
- archive construction or extraction;
- security analyzer and policy changes;
- revocation override behavior;
- Markdown rendering and CSP;
- future authentication, authorization, signing, submission, or live-preview work.

---

## 8. Assumptions & Accepted Risks

### Security assumptions

1. Production public endpoints use HTTPS at an operator-controlled edge.
2. Private operators enforce network or proxy authorization before exposing private content.
3. Operators protect Marketplace server configuration, database, blob storage, and backups.
4. Client organization policy is authoritative for allowed Marketplace and Git origins.
5. SHA-256 remains collision-resistant for artifact integrity.

### Accepted v1 risks

1. **No built-in authorization:** accepted to keep v1 single-policy-domain and read-only; private deployments require external protection.
2. **No signatures:** HTTPS, stable instance identity, and digests provide initial integrity; independent signing and key distribution are deferred.
3. **No semantic safety proof:** structural analysis cannot establish that agent instructions are benign. Provenance, findings, consent, and revocation reduce but do not remove this risk.
4. **No volumetric DDoS defense:** application limits cover bounded resource exhaustion; CDN/WAF/network controls cover volumetric attacks.
5. **File-administered approval has external audit identity:** Git or deployment configuration review must supply author identity until submissions/admin workflows exist.
6. **Revocation cannot recall immutable bytes:** Online resolution and synchronization refuse known revoked revisions, but clients and intermediary caches may retain bytes already acquired; offline clients use cached revocation state and warn that it may be stale.

---

## 9. Threat Model Changelog

### Version 1.0.0 (2026-08-18)

- Created the repository threat model.
- Covered the current Git-based CLI and approved Marketplace serve architecture.
- Completed STRIDE analysis for client, worker, API, storage, frontend, and materialization boundaries.
- Recorded v1 accepted risks and security testing gates.

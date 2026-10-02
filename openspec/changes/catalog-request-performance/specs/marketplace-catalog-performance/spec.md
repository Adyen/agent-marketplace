## Purpose

Preserves public catalog correctness and authoritative failure handling during performance optimization, and defines bounded operations timing without changing existing metric semantics.

## ADDED Requirements

### Requirement: Catalog optimization preserves bounded page responses

The Marketplace SHALL preserve catalog response semantics on both supported metadata backends.

#### Scenario: Supported catalog page sizes and conditional responses

- **WHEN** a client requests the first catalog page with limits 1, 100, or 500, a continuation page, or a matching first-page conditional request
- **THEN** the response MUST preserve the requested page bound, resource ordering, cursor and completion semantics, and bodyless 304 behavior
- **AND** empty and final pages MUST remain valid responses

### Requirement: Operations timing covers the public request stack

Operations metrics SHALL expose bounded full-stack and phase timing while preserving the meaning of existing request metric series.

#### Scenario: Early failures remain observable without sensitive labels

- **WHEN** public requests succeed or fail at configuration authority, managed availability, or catalog processing
- **THEN** the operations listener MUST expose full-stack and applicable phase timing
- **AND** raw paths, resource identities, credentials, and sensitive values MUST NOT become metric labels
- **AND** the public listener MUST NOT expose metrics or profiling endpoints

### Requirement: Managed availability is coherent within a request

Each public request SHALL use one coherent managed configuration state without sharing a mutable snapshot with another request.

#### Scenario: Concurrent policy changes preserve request isolation

- **WHEN** managed policy changes while a public request is in flight
- **THEN** that request MUST NOT mix managed configuration revisions
- **AND** a subsequent request MUST observe the updated state
- **AND** concurrent requests MUST remain isolated

### Requirement: Authority failures remain fail closed

Public configuration authority and managed availability failures SHALL refuse ordinary responses rather than serve stale success.

#### Scenario: Unavailable or invalid authority rejects public requests

- **WHEN** adoption is required, required safety capabilities are absent, managed state is malformed, or the metadata store is unavailable
- **THEN** affected public requests MUST return 503 with non-cacheable failure headers
- **AND** liveness exemption, admin authority, startup checks, and readiness checks MUST retain their existing behavior

### Requirement: Conditional requests retain their wire contract

Conditional catalog reads SHALL preserve first-page GET and HEAD validator behavior.

#### Scenario: Validators preserve matching and rejection behavior

- **WHEN** a first-page GET or HEAD supplies a matching strong, weak, list, or wildcard validator
- **THEN** the response MUST remain a bodyless 304 with the applicable catalog headers
- **AND** malformed validators MUST retain their rejection behavior
- **AND** a continuation request with If-None-Match MUST return 400

### Requirement: Conditional responses require current authoritative state

A catalog validator SHALL NOT hide a visible state change, an authoritative read failure, or corruption that the existing catalog scan rejects.

#### Scenario: Mutation and unchanged-version corruption cannot produce stale 304

- **WHEN** catalog publication, current assessments, registry listing or provenance, collections, managed policy or withdrawal, or effective revocation changes
- **THEN** an obsolete validator MUST NOT produce a stale 304
- **AND WHEN** authoritative state is unavailable or scan-rejected findings or collection data becomes corrupt without a catalog-version increment
- **THEN** the conditional request MUST fail rather than return 304

### Requirement: Optimized responses preserve validation and sanitization

Performance optimizations SHALL retain applicable public-data validation and source sanitization.

#### Scenario: Invalid data is rejected and valid sources remain sanitized

- **WHEN** a catalog response contains invalid public resource data or a source containing sensitive URL components
- **THEN** applicable validation failures MUST retain their rejection behavior
- **AND** valid public source data MUST omit sensitive URL components
- **AND** valid controls MUST still produce the expected response

### Requirement: Winning revisions and page boundaries remain deterministic

Catalog pages SHALL retain the eligible winning resource revision for each resource coordinate in stable keyset order.

#### Scenario: Duplicate and legacy candidates preserve winning pages

- **WHEN** multiple eligible resource revisions, legacy rows, missing assessment or occurrence candidates, or an atomic publication affect the catalog
- **THEN** each coordinate MUST retain the existing lexicographic winning content digest and eligibility rules
- **AND** ordered page boundaries and lookahead MUST NOT omit or duplicate eligible coordinates within an unchanged catalog

### Requirement: Moving scans remain stateless and backend compatible

Catalog scans SHALL retain their wire fields, statuses, headers, collection relations, and stateless cursor semantics on PostgreSQL and SQLite.

#### Scenario: Publication between pages preserves stateless continuation

- **WHEN** a client follows a catalog cursor after a publication changes the catalog
- **THEN** continuation MUST reflect the current eligible catalog after the cursor coordinate without requiring retained server scan state
- **AND** mixed resource kinds, collection relations, final-page completion, and ETag meaning MUST remain backend compatible

### Requirement: Catalog reads preserve scope and metadata

Catalog requests SHALL remain read-only and scoped to the requested Marketplace.

#### Scenario: Reads do not mutate authoritative state or escape the Marketplace

- **WHEN** successful, conditional, rejected, or failed catalog requests run against a store containing another Marketplace
- **THEN** metadata and blob state MUST remain unchanged
- **AND** results MUST remain within the requested Marketplace and retain authority and effective revocation boundaries

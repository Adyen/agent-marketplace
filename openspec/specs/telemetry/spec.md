# telemetry Specification

## Purpose
Define telemetry consent and policy authority, endpoint routing, bounded disclosure, diagnostic review, and inspectable local state without changing command outcomes.

## Requirements

### Requirement: Telemetry requires explicit policy

Telemetry SHALL default to `off`. Persistent telemetry levels SHALL be `off`, `basic`, or `detailed`; a diagnostic session SHALL be a per-invocation overlay, not a persistent level. Unmanaged interactive initialization SHALL permit the user to decline community telemetry consent. Noninteractive initialization SHALL NOT infer consent.

#### Scenario: Unconfigured telemetry stays off

- **WHEN** no organization or user setting enables telemetry
- **THEN** the effective telemetry level MUST be `off` and no telemetry providers MUST send data

#### Scenario: Initialization does not imply consent

- **WHEN** unmanaged initialization is noninteractive or interactive consent is declined
- **THEN** it MUST NOT enable telemetry implicitly

### Requirement: Organization telemetry policy retains authority

An explicit organization telemetry level SHALL determine the effective level in both directions. Organization endpoint and community-report settings SHALL override user preferences. User environment overrides SHALL apply only where organization policy permits them; any organization telemetry block SHALL suppress the environment level override. Endpoint headers and CA settings SHALL come from the authority that supplied the endpoint. Standard `OTEL_*` environment variables SHALL NOT override this policy.

#### Scenario: Organization level is authoritative

- **WHEN** organization and user telemetry levels differ
- **THEN** the explicit organization level MUST determine the effective level

#### Scenario: Organization policy gates environment overrides

- **WHEN** telemetry environment overrides coexist with organization policy
- **THEN** level, endpoint, and community-report overrides MUST obey the organization's field authority

#### Scenario: Endpoint authentication follows endpoint authority

- **WHEN** the organization chooses the primary telemetry route
- **THEN** user endpoint authentication MUST NOT be applied to that route

#### Scenario: Standard OpenTelemetry endpoint variables are ignored

- **WHEN** standard `OTEL_*` endpoint variables are set
- **THEN** delivery MUST continue to the configured Agent Marketplace endpoint

### Requirement: Primary and community routes remain distinct

The primary telemetry route SHALL use the organization endpoint when configured, otherwise the user endpoint, otherwise the configured project endpoint. A separately authorized community report SHALL carry only basic telemetry to the project endpoint and SHALL be disabled when telemetry is off or no project endpoint exists. Equivalent primary and community destinations SHALL be folded to avoid duplicate delivery.

#### Scenario: User endpoint and community report coexist

- **WHEN** a user selects a primary endpoint and enables a community report
- **THEN** the primary route MUST retain the user endpoint and the community route MUST use the configured project endpoint

#### Scenario: Equivalent routes are delivered once

- **WHEN** primary telemetry and the community report have equivalent destinations
- **THEN** the community report MUST be folded into primary delivery

### Requirement: Telemetry transport protects endpoint credentials

Telemetry endpoints SHALL use HTTPS or loopback HTTP unless organization policy explicitly permits private-network HTTP. Private hostnames SHALL be checked at delivery and MUST NOT resolve to public addresses. Authentication headers SHALL read exactly one environment or file source rather than inline values. Credential and CA files SHALL be private regular files, not symlinks, with private owned parent directories. Endpoint environment overrides SHALL NOT forward configured credentials to a different destination.

#### Scenario: Endpoint transport follows organization permission

- **WHEN** public, private, loopback, or unresolved HTTP endpoints are validated
- **THEN** only endpoints permitted by transport policy MUST be deliverable

#### Scenario: Header and CA files require private ownership

- **WHEN** telemetry authentication or CA files have unsafe permissions, ownership, or symlink structure
- **THEN** validation MUST refuse them while accepting private regular-file controls

#### Scenario: Endpoint override does not forward authentication

- **WHEN** the environment changes the primary destination
- **THEN** configured authentication MUST NOT leak to the replacement destination

### Requirement: Basic telemetry has bounded disclosure

Basic telemetry SHALL report command invocation counts and durations, with command path, harness targets, managed status, and success/error outcome. Resource attributes SHALL carry schema version, CLI version, operating system, architecture, and a random persistent installation ID. Command arguments, resource contents, and arbitrary instrument attributes SHALL NOT be collected by these metrics. Initialization and telemetry-control commands SHALL NOT report themselves.

#### Scenario: Commands report basic metrics without argument data

- **WHEN** a basic-telemetry command succeeds or fails
- **THEN** its command metrics MUST carry the bounded invocation attributes and outcome without promoting installation identity into instrument labels

#### Scenario: Telemetry controls do not self-report

- **WHEN** telemetry-control commands run
- **THEN** they MUST NOT send command telemetry

### Requirement: Detailed inventory is complete bounded primary-route evidence

Detailed telemetry SHALL add lifecycle traces and effective-configuration inventory to the primary telemetry route, never the community report. Inventory SHALL use snapshot, registry, and resource gauges plus an omission counter. Registry locations SHALL omit credentials, query strings, and fragments; local locations SHALL be represented as `local`, not path-derived identities. Fingerprints SHALL be deterministic over represented configuration fields. Snapshot observation keys SHALL increase within one runtime, and repeated observations SHALL retain ordered delivery. Hard resolution failures SHALL suppress inventory; recording SHALL NOT block commands on network delivery. A snapshot exceeding 1,000 registries, 10,000 resources, or 512 UTF-8 bytes per string SHALL be omitted as a whole.

#### Scenario: Inventory is detailed primary only

- **WHEN** detailed telemetry and a community report are both enabled
- **THEN** configuration inventory MUST reach only the detailed primary route and MUST be absent at basic level

#### Scenario: Inventory sanitizes registry identity

- **WHEN** effective inventory contains remote URLs and local registry paths
- **THEN** remote credentials and suffixes MUST be removed and local filesystem identities MUST NOT be disclosed

#### Scenario: Fingerprints and observation order are stable

- **WHEN** equivalent configurations are presented in different orders or observed repeatedly
- **THEN** their fingerprints MUST remain equal and observations MUST retain increasing keys and ordered delivery

#### Scenario: Oversized inventory is omitted whole

- **WHEN** a snapshot exceeds a registry, resource, or string bound
- **THEN** partial inventory MUST NOT be sent and the bounded omission reason MUST identify the failure

#### Scenario: Resolution and export failures do not become inventory success

- **WHEN** resolution fails hard or telemetry export stalls
- **THEN** hard failures MUST produce no inventory snapshot and recording MUST NOT wait for network export

### Requirement: Diagnostic sessions require review and explicit export

A diagnostic session SHALL operate only at effective level `detailed`, buffer evidence locally, redact URL credentials, and write a review payload under the telemetry diagnostic directory. Export SHALL require interactive consent or explicit noninteractive sensitive-data acknowledgement. Successful diagnostic completion SHALL require both endpoint delivery and review-payload deletion; declined or failed delivery SHALL retain the payload. A payload deletion failure after delivery SHALL report its filesystem cause and retained payload path without claiming successful completion. Diagnostic failure SHALL NOT change the investigated command's outcome. Diagnostic payloads SHALL NOT enter the normal pending telemetry queue.

#### Scenario: Diagnostics require detailed telemetry

- **WHEN** a diagnostic session is requested at off, basic, or detailed level
- **THEN** capture MUST occur only at detailed level

#### Scenario: Diagnostic failure retains redacted review evidence

- **WHEN** diagnostic export fails
- **THEN** the retained payload MUST identify the investigated trace and installation while redacting URL credentials

#### Scenario: Noninteractive diagnostics require acknowledgement

- **WHEN** diagnostic export runs without a terminal or explicit acknowledgement
- **THEN** the payload MUST remain local and the CLI MUST explain the acknowledgement requirement

#### Scenario: Successful diagnostic export removes review evidence

- **WHEN** explicitly accepted diagnostic export succeeds
- **THEN** the local review payload MUST be deleted

#### Scenario: Failed diagnostic export is never queued

- **WHEN** an accepted diagnostic export reaches an endpoint that refuses it
- **THEN** the payload MUST remain reviewable and MUST NOT create a pending telemetry record

#### Scenario: Diagnostic payload cleanup failure is reported

- **WHEN** accepted diagnostic delivery succeeds but removing the local review payload fails
- **THEN** diagnostic completion MUST report the filesystem error and retained path without reporting success
- **AND** the retained payload bytes MUST remain unchanged without staging residue

### Requirement: Local telemetry state is inspectable and resettable

Telemetry state SHALL live under `$XDG_STATE_HOME/agent-marketplace/telemetry`, defaulting to `~/.local/state/agent-marketplace/telemetry`. Installation IDs SHALL persist privately and be resettable. Pending telemetry records SHALL have a default limit of 5 MiB or seven days, evicting oldest first, and SHALL be retried in order. Status SHALL expose effective policy, route, identity, pending records, and failures. Preview SHALL write local sample evidence without delivery. Purge SHALL remove queued records and retained diagnostics; reset SHALL additionally replace installation identity only after cleanup succeeds. Reset cleanup failure SHALL propagate its filesystem cause without claiming success or creating a replacement ID, and MAY leave the old ID and queued records already removed.

#### Scenario: Installation identity persists privately and resets

- **WHEN** installation identity is loaded repeatedly and then reset
- **THEN** it MUST remain stable with owner-only access until reset, which MUST clear queued and retained diagnostic evidence and permit a new identity

#### Scenario: Pending records are bounded and retried in order

- **WHEN** pending records exceed size or age limits or become deliverable
- **THEN** default retention MUST be 5 MiB and seven days, oldest records MUST be evicted first, and delivery MUST preserve queue order

#### Scenario: Status and preview expose local evidence

- **WHEN** users inspect telemetry status or request a preview
- **THEN** status MUST expose local delivery evidence and preview MUST write a sample payload without sending it

#### Scenario: Purge removes queued and diagnostic evidence

- **WHEN** telemetry purge runs with queued records and retained diagnostics
- **THEN** it MUST remove both and report their counts

#### Scenario: Installation ID reset cleanup failure is reported

- **WHEN** installation-ID reset cannot remove retained diagnostic evidence after removing the old ID and queued records
- **THEN** the reset command MUST fail with the filesystem cause, preserve remaining diagnostic bytes and metadata, and neither claim success nor create a replacement ID
- **AND** a retry after restoring directory permissions MUST finish cleanup and create a new installation ID

### Requirement: Interactive sessions record one final inventory

An interactive TUI session SHALL record effective-configuration inventory at most once, when the session ends, using the latest accepted complete snapshot of that session. Snapshots superseded during the session, including those reloaded after toggles, edits, sync, or update, SHALL NOT be recorded, and operations the session runs SHALL NOT record their own inventory. When the latest accepted snapshot has hard resolution failures, the session SHALL record no inventory. A session terminated without running its exit path MAY record nothing; a later command records the current configuration.

#### Scenario: Interactive edits record only the final configuration

- **WHEN** a detailed-telemetry TUI session accepts several successive snapshots with different configurations
- **THEN** no inventory MUST be recorded before the session ends and exactly one snapshot, for the last accepted configuration, MUST be recorded when it ends

#### Scenario: Session operations do not record inventory

- **WHEN** an operation started by the TUI session resolves configuration
- **THEN** that operation MUST NOT record inventory

#### Scenario: Failed final snapshot records nothing

- **WHEN** the last accepted snapshot of a TUI session has hard resolution failures
- **THEN** the session end MUST record no inventory

## MODIFIED Requirements

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

## ADDED Requirements

### Requirement: Installation refresh health is observable

The operations metrics endpoint SHALL expose refresh attempt outcomes with bounded labels, refresh duration and the last successful evaluation timestamp. Background attempt failures SHALL be logged once per attempt rather than once per affected search. Metrics and logs SHALL NOT disclose Installation IDs, registry signatures, endpoint credentials or backend response payloads.

#### Scenario: Refresh health distinguishes attempts from search fallback

- **WHEN** background refreshes succeed or fail while searches reuse counts or fall back
- **THEN** operations metrics SHALL reflect attempt outcomes, duration and the last successful evaluation timestamp without search fallback creating additional attempt observations
- **AND** failures SHALL produce per-attempt bounded logs without sensitive identifiers or credentials

## MODIFIED Requirements

### Requirement: Installation metrics timing is independently configurable

Marketplace operator configuration SHALL accept optional `installation_metrics.refresh.interval`, `installation_metrics.refresh.retry_interval`, `installation_metrics.refresh.jitter`, `installation_metrics.max_age` and `installation_metrics.timeout` with defaults of four minutes, thirty seconds, fifteen seconds, five minutes and three seconds respectively. Successful refresh scheduling SHALL use the evaluation time plus interval and additional jitter between zero and its configured bound, or just after an earlier inventory validity horizon. A refresh loop SHALL have at most one active attempt. Unavailable-count or identity-change search hints SHALL be non-blocking and coalesced, SHALL NOT repeat work for a still-fresh compatible aggregate, and SHALL NOT bypass the minimum failure retry delay measured from attempt completion. Construction and handler access SHALL NOT start refresh work; API shutdown SHALL cancel outstanding refresh work and await its termination. Metrics without a configured URL SHALL remain disabled and SHALL NOT affect readiness. Registry reindex timing SHALL remain independent.

#### Scenario: Existing configuration uses defaults independently of reindexing

- **WHEN** an operator leaves the new installation metrics timing settings absent or changes only registry reindex timing
- **THEN** installation refresh SHALL use its own four-minute interval, thirty-second retry, fifteen-second jitter, five-minute maximum age and three-second timeout
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


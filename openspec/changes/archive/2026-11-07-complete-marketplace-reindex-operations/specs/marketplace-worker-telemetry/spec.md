## ADDED Requirements

### Requirement: Worker processes serve worker metrics on the operations listener

A `serve` process that runs the worker SHALL serve the worker metrics at `/metrics` on its operations listener. A `--role worker` process SHALL open only the operations listener and SHALL serve no route other than `/metrics` there. A `--role all` process SHALL serve each worker series exactly once on its existing operations `/metrics`. The public listener SHALL NOT serve worker metrics.

#### Scenario: A worker-only process serves worker metrics

- **WHEN** `serve --role worker` runs with `operations_listen` set and has observed a configured registry
- **THEN** `GET /metrics` on the operations address returns the source observation, refresh, host-budget delay, configuration candidate, and restart-required series, `/readyz` returns `404`, and nothing listens on the `listen` address

#### Scenario: An all-role process exposes each worker series once

- **WHEN** `serve --role all` runs and its operations `/metrics` is scraped
- **THEN** the response contains the existing API series and each worker series name exactly once, and the public listener's `/metrics` returns `404`

### Requirement: Worker signals distinguish operational conditions

Metrics and structured logs SHALL distinguish an unchanged healthy observation, a source failure, a refresh failure, a configuration rejection, and a host-budget delay. A source failure SHALL NOT be counted as a refresh failure, a host-budget delay SHALL NOT be counted as either failure, and a refresh that a newer indexing generation superseded SHALL be counted as `superseded`, not `failed`. Every finished refresh SHALL produce one `refresh finished` log record carrying the registry, trigger, outcome, and duration.

#### Scenario: An unchanged observation is reported as healthy

- **WHEN** the worker observes a registry whose tracked ref is unchanged since its last successful index
- **THEN** the `unchanged` observation count increases, the registry's last successful observation timestamp advances, its consecutive probe failure gauge is `0`, and no refresh count changes

#### Scenario: A source failure is distinct from a refresh failure

- **WHEN** one registry's ref resolution fails and another registry's clone or ingestion fails during its refresh
- **THEN** the first increases the `failed` observation count and its consecutive probe failure gauge without changing any refresh count or its last successful index timestamp, and the second increases the `failed` refresh count for its trigger and logs `refresh finished` with outcome `failed` without changing the `failed` observation count

#### Scenario: A host-budget delay is counted separately

- **WHEN** a due observation or a refresh is refused admission by a saturated host budget
- **THEN** the host-budget delay count for that operation increases, and neither the `failed` observation count nor the `failed` refresh count changes

#### Scenario: A configuration rejection is distinct from restart-required drift

- **WHEN** a running worker sees an invalid configuration replacement, and later a valid replacement that changes only a restart-required field
- **THEN** the first increases the `rejected` configuration candidate count and leaves the restart-required gauge at `0`, and the second sets the restart-required gauge to `1` without increasing the `rejected` count

#### Scenario: A superseded refresh is not a refresh failure

- **WHEN** a refresh started under an indexing generation that a newer applied candidate superseded is prevented from publishing
- **THEN** the `superseded` refresh count increases and the `failed` refresh count does not

### Requirement: Worker telemetry is low-cardinality and secret-free

Worker metric label values SHALL come only from fixed value sets and operator-configured registry IDs. Origins, hosts, commits, indexing generations, error text, job IDs, and credential values SHALL NOT appear in metric names, label values, or the metrics response. Per-registry series SHALL be removed when the registry leaves the runtime configuration.

#### Scenario: Worker metrics contain no credentials or unbounded values

- **WHEN** a registry whose origin and token environment variable carry secret values is observed, refreshed, and fails once
- **THEN** the metrics response and the worker's log records contain neither secret, and every worker label value is a configured registry ID or one of its metric's fixed values

#### Scenario: A removed registry's series are dropped

- **WHEN** a registry is removed from the runtime configuration of a running worker
- **THEN** the next metrics response contains no per-registry series labelled with that registry's ID

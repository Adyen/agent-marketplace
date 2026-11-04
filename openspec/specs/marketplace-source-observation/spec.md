# marketplace-source-observation Specification

## Purpose
The Marketplace worker observes each configured registry repository cheaply through Git and acquires, ingests, and publishes only when the indexed result may change, while bounding upstream Git traffic and preserving the last successfully published index.

## Requirements

### Requirement: Remote ref resolution uses the hardened acquisition path

The Marketplace SHALL resolve a registry's configured ref to a commit through the same Git execution path it uses for acquisition: the same origin and ref validation, hardened Git arguments, credential environment, custom CA, timeout, output bounds, and redaction. Resolution MUST NOT create a checkout or download the repository tree. A ref naming a branch SHALL resolve to that branch's tip commit. A ref naming an annotated tag SHALL resolve to the commit the tag points at, not the tag object, and a lightweight tag to its commit. A full lowercase commit SHALL resolve to itself without contacting the origin. A short ref that names both a branch and a tag MUST fail as ambiguous. A ref the origin does not advertise MUST fail. No resolution error or log record MAY contain a credential value.

#### Scenario: A branch resolves to its tip commit

- **WHEN** a registry tracks a branch in a local fixture repository whose branch has advanced to a new commit
- **THEN** resolving the ref returns the new tip commit

#### Scenario: An annotated tag resolves to the peeled commit

- **WHEN** a registry tracks an annotated tag in a local fixture repository
- **THEN** resolving the ref returns the commit the tag points at, not the tag object

#### Scenario: A full commit resolves without contacting the origin

- **WHEN** a registry tracks a full commit and its origin is unreachable
- **THEN** resolving the ref returns that commit and runs no Git command

#### Scenario: A short ref naming both a branch and a tag fails as ambiguous

- **WHEN** a registry tracks a short ref name that exists as both a branch and a tag in the origin
- **THEN** resolution fails with an ambiguity error and returns no commit

#### Scenario: Ref resolution errors and logs never contain credentials

- **WHEN** ref resolution with a token credential and an origin carrying URL credentials fails against an unreachable local origin
- **THEN** neither the returned error nor any log record contains the token or the URL password

### Requirement: An unchanged source performs no acquisition

When an observation resolves the same commit as the registry's last successful index and the registry's configuration is unchanged since that index, the worker SHALL NOT clone, check out, ingest, publish blobs, or update the registry head or refresh status. A failed observation, including a missing ref, MUST leave the published registry head, its resources, and its refresh status unchanged and SHALL record the failure as observation health only.

#### Scenario: Repeated unchanged observations perform no acquisition

- **WHEN** a registry whose tracked branch is unchanged since its last successful index is observed repeatedly
- **THEN** no further acquisition happens and the registry head, its blobs, and its refresh status are unchanged

#### Scenario: A missing ref preserves the last published index

- **WHEN** a successfully indexed registry's tracked ref is deleted from its origin and the registry is observed
- **THEN** the observation records a probe failure, no acquisition happens, and the registry head, its resources, and its refresh status are unchanged

### Requirement: Each refresh trigger causes one bounded refresh

The worker SHALL request exactly one refresh of a registry when an observation finds a commit different from the last successfully indexed commit, when the registry's startup configuration differs from the configuration of its last successful index, when the registry's periodic reconciliation is due, or when an operator runs `serve reindex` for it, even if commit and configuration are unchanged. Periodic reconciliation SHALL occur at least once per reconciliation interval, which defaults to 24 hours. A refresh job SHALL have a bounded number of attempts. A trigger that targets the same commit and configuration as a refresh that is still active MUST coalesce onto that refresh instead of creating another.

#### Scenario: A changed commit triggers exactly one refresh

- **WHEN** a successfully indexed registry's tracked branch advances to a new commit and the registry is observed several times before the refresh runs
- **THEN** exactly one refresh runs, it publishes the new commit, and later observations of that commit perform no acquisition

#### Scenario: A manual reindex refreshes an unchanged registry once

- **WHEN** an operator runs `serve reindex` for a registry whose commit and configuration are unchanged since its last successful index
- **THEN** exactly one refresh of that registry runs and completes

#### Scenario: A due reconciliation refreshes an unchanged registry once

- **WHEN** a registry whose commit and configuration are unchanged reaches its reconciliation time and is observed
- **THEN** exactly one refresh of that registry runs, and its next reconciliation is scheduled one reconciliation interval later

#### Scenario: Concurrent triggers coalesce onto one refresh

- **WHEN** a changed-commit observation, a reconciliation, and a manual reindex target the same commit and configuration of one registry while its refresh is still active
- **THEN** only one refresh job exists for that registry and every trigger reports that job

### Requirement: Observation scheduling and backoff are deterministic

After a successful observation the worker SHALL schedule the registry's next observation at a stable phase of `refresh.interval` that depends only on the registry ID, so the same registry keeps the same phase across worker restarts and different registries spread across the interval. A registry never observed before SHALL be due immediately. After consecutive failed observations the worker SHALL delay the next observation of that registry by a backoff that depends only on the registry ID and the failure count, that grows with the failure count, and that never exceeds the reconciliation interval. A successful observation SHALL reset the failure count and return the registry to its stable phase. When the host budget refuses admission for a due observation, the worker SHALL leave the registry due and SHALL log the delay with the registry ID, a redacted host, and how long the observation has been overdue.

#### Scenario: Stable phases are deterministic across restarts

- **WHEN** two registries are observed successfully, the worker restarts, and they are observed successfully again
- **THEN** each registry's next observation time has the same offset within `refresh.interval` before and after the restart, and the two offsets differ

#### Scenario: Probe failure backoff is deterministic and capped

- **WHEN** a registry's observation fails repeatedly
- **THEN** each delay is the same for the same registry and failure count, the delays grow with the failure count, no delay exceeds the reconciliation interval, and the next successful observation resets the failure count

#### Scenario: Host budget delay is visible

- **WHEN** a registry is due but its host budget has no free operation slot
- **THEN** no observation of that registry runs, it remains due, and the worker logs a host-budget delay naming the registry and the overdue duration

### Requirement: Multiple workers do not multiply upstream requests

Workers that share one metadata store SHALL observe each due registry at most once per due time and SHALL hold the registry work lease for every observation and refresh. Every remote Git operation, observation and acquisition alike, SHALL be admitted by the deployment-wide host budget for the registry's normalized source host, which allows at most two concurrent operations and at most one concurrent acquisition per host. Local origins SHALL use a budget separate from remote hosts.

#### Scenario: Two workers observe each due registry once

- **WHEN** two workers share one metadata store and several registries become due
- **THEN** each registry is observed exactly once for that due time and no registry is refreshed twice for the same commit and configuration

#### Scenario: Host operations stay within the budget across workers

- **WHEN** two workers concurrently observe and refresh registries that share one source host
- **THEN** at no point do more than two remote operations or more than one acquisition run against that host

### Requirement: Source observation telemetry contains no secrets

The worker SHALL log every observation outcome (changed, unchanged, or failed), every refresh request with its trigger (`source-change`, `config-change`, `reconciliation`, or `manual`) and whether it coalesced, and every host-budget delay, with the registry ID and duration. Origins and hosts in these records SHALL be redacted. No observation or refresh-request log record MAY contain a token, password, private key path content, authorization header, or URL credentials.

#### Scenario: Observation telemetry contains no credentials

- **WHEN** registries configured with a token credential and an origin carrying URL credentials are observed with changed, unchanged, and failed outcomes
- **THEN** a log record exists for each outcome and refresh request, and none of them contains the token, the URL password, or an authorization header

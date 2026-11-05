# Spec Delta

## Purpose

Durable registry observation state in the Marketplace metadata store lets workers coordinate when to observe and refresh each registry: one exclusive registry work lease, deployment-wide host budgets, and coalesced refresh requests, with identical behavior on SQLite and PostgreSQL.

## ADDED Requirements

### Requirement: Registry observation state is durable and separate from published registry state

The Marketplace metadata store SHALL keep one registry observation record per registry, holding observed, desired, and successfully indexed state, probe health, scheduling times, the registry work lease, and the active refresh. Creating, updating, or removing observation state MUST NOT change the registry's published record, its head, or its immutable occurrences, resource revisions, or assessments. An existing SQLite or PostgreSQL metadata database SHALL upgrade in place without losing existing data, and repeating the upgrade SHALL change nothing.

#### Scenario: Observation writes leave published registry state unchanged

- **WHEN** a registry with a published head and immutable occurrences gains an observation record that is then updated, leased, given an active refresh, and removed
- **THEN** the registry record, its head, and its occurrences, resource revisions, and assessments read back exactly as they did before

#### Scenario: Existing metadata database upgrades without data loss

- **WHEN** a metadata database created at the schema version preceding this change, holding registries, occurrences, heads, and jobs, is opened by this version
- **THEN** every pre-existing record reads back unchanged and observation records can be created for its registries

#### Scenario: Repeating the schema upgrade is a no-op

- **WHEN** an upgraded metadata database is opened again
- **THEN** no further migration is applied and all registry and observation records are unchanged

### Requirement: The registry work lease is exclusive

A worker SHALL obtain the registry work lease before observing or refreshing a registry. While a lease is held and unexpired, the metadata store MUST refuse it to every other claimant. An expired lease SHALL be reclaimable by any worker, and a holder whose lease expired or was reclaimed MUST be refused when it tries to renew or release that lease.

#### Scenario: A second claimant is refused a held lease

- **WHEN** worker A holds an unexpired registry work lease and worker B requests the same registry's lease
- **THEN** worker B's request is refused with a conflict and worker A still holds the lease

#### Scenario: An expired lease is reclaimable

- **WHEN** worker A's registry work lease has expired and worker B requests the lease
- **THEN** worker B holds the lease

#### Scenario: A stale holder cannot renew or release the lease

- **WHEN** worker B has reclaimed a registry work lease that worker A previously held, and worker A tries to renew or release its former lease
- **THEN** both attempts are refused and worker B still holds the lease with its own expiry

### Requirement: Due observation selection is deterministic

Selecting due observations for a given clock time SHALL return only registries whose next probe or reconciliation time is at or before that time and whose work lease is not held, ordered by earliest due time and then by registry ID, truncated to the requested limit. The same stored state and clock time MUST yield the same ordered result.

#### Scenario: Due observations come back in a stable order

- **WHEN** several observations are due at different and equal times and selection runs twice with the same clock time
- **THEN** both runs return the same registries in earliest-due-then-registry-ID order

#### Scenario: Leased and not-yet-due observations are not selected

- **WHEN** one due observation's registry work lease is held, another observation is not yet due, and selection runs
- **THEN** neither of those registries is returned

### Requirement: Observation updates use compare-and-swap

Every observation update SHALL name the observation version it read. The metadata store MUST reject an update whose expected version is no longer current, leaving every field of the stored observation unchanged, and SHALL advance the version on each accepted update.

#### Scenario: A stale observation update is rejected without partial write

- **WHEN** two writers read the same observation version, the first update succeeds, and the second writer submits an update based on the old version
- **THEN** the second update is refused with a conflict and the stored observation equals the first writer's result in every field

### Requirement: Host budgets are deployment-wide

The metadata store SHALL admit remote operations against a normalized source host only within that host's budget of concurrent operations and concurrent acquisitions, counted across every worker that shares the store. Admission beyond capacity MUST be refused, including when a worker that already holds an admission asks again. Released or expired admissions SHALL free their capacity.

#### Scenario: Admission beyond host capacity is refused across workers

- **WHEN** different workers already hold every concurrent-operation slot for a host, or every acquisition slot, and another worker requests an operation of that kind on the same host
- **THEN** the request is refused and the existing admissions are unchanged

#### Scenario: Reacquisition cannot exceed host capacity

- **WHEN** a worker that already holds admissions filling a host's capacity requests another admission on that host
- **THEN** the request is refused and the host's admitted count does not exceed its capacity

#### Scenario: Released or expired admissions free host capacity

- **WHEN** a host is at capacity and one admission is released or reaches its expiry
- **THEN** a subsequent request for that host is admitted

### Requirement: Refresh requests coalesce onto one active refresh

Requesting a refresh SHALL update the registry's desired commit and indexing generation and its active refresh in one atomic step: either both change or neither does. When an active refresh already targets the same desired commit and generation, the request SHALL coalesce onto it instead of creating another refresh job. When a refresh job fails terminally, the metadata store SHALL clear it as the registry's active refresh so a later request can create a new one.

#### Scenario: A failed refresh request changes neither desired state nor the active refresh

- **WHEN** a refresh request fails partway through, after desired state would change but before its refresh job is recorded
- **THEN** the observation's desired state, active refresh, and version are unchanged and no refresh job was added

#### Scenario: Concurrent equivalent refresh requests coalesce into one active refresh

- **WHEN** several workers concurrently request a refresh of the same registry for the same desired commit and generation
- **THEN** exactly one refresh job exists, it is the observation's active refresh, and every request reports that same job

#### Scenario: A terminal refresh failure clears the active refresh

- **WHEN** the active refresh job of a registry fails for the last permitted attempt and a refresh is then requested again
- **THEN** the observation has no active refresh after the failure, and the new request creates a new active refresh job

### Requirement: Removing observation state preserves immutable records

Removing a registry's observation record SHALL delete only that scheduler state. The registry's immutable occurrences, resource revisions, assessments, and head MUST remain readable.

#### Scenario: Removing an observation keeps immutable records

- **WHEN** the observation record of a registry with published occurrences, resource revisions, and assessments is removed
- **THEN** reading that observation reports not found and every occurrence, resource revision, assessment, and the head still read back unchanged

### Requirement: SQLite and PostgreSQL satisfy one observation contract

Both metadata backends SHALL satisfy every requirement of this capability with the same observable results for the same inputs.

#### Scenario: Both backends pass the same observation conformance cases

- **WHEN** the same observation conformance cases run against SQLite and against PostgreSQL
- **THEN** each case produces the same independently stated expected result on both backends, and a PostgreSQL run that does not execute counts as a failure

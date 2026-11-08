# Spec Delta

## MODIFIED Requirements

### Requirement: Refresh requests coalesce onto one active refresh

Requesting a refresh SHALL update the registry's desired commit and indexing generation and its active refresh in one atomic step: either both change or neither does. An active refresh is a refresh job that can still run: a pending job, a failed job with attempts remaining, or a leased job that has attempts remaining or whose lease is still valid. When an active refresh already targets the same desired commit and generation, the request SHALL coalesce onto it instead of creating another refresh job. When a refresh job fails terminally, the metadata store SHALL clear it as the registry's active refresh so a later request can create a new one.

#### Scenario: A failed refresh request changes neither desired state nor the active refresh

- **WHEN** a refresh request fails partway through, after desired state would change but before its refresh job is recorded
- **THEN** the observation's desired state, active refresh, and version are unchanged and no refresh job was added

#### Scenario: Concurrent equivalent refresh requests coalesce into one active refresh

- **WHEN** several workers concurrently request a refresh of the same registry for the same desired commit and generation
- **THEN** exactly one refresh job exists, it is the observation's active refresh, and every request reports that same job

#### Scenario: A terminal refresh failure clears the active refresh

- **WHEN** the active refresh job of a registry fails for the last permitted attempt and a refresh is then requested again
- **THEN** the observation has no active refresh after the failure, and the new request creates a new active refresh job

#### Scenario: An expired lease on the final permitted attempt does not coalesce a refresh request

- **WHEN** a worker leases the active refresh job on its final permitted attempt, its lease expires without the worker recording an outcome, and a refresh is requested for the same desired commit and generation
- **THEN** the request creates a new active refresh job and does not report the expired job as coalesced

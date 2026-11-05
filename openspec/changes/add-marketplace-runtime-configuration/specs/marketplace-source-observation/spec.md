# Spec Delta

## MODIFIED Requirements

### Requirement: Each refresh trigger causes one bounded refresh

The worker SHALL request exactly one refresh of a registry when an observation finds a commit different from the last successfully indexed commit, when the registry's indexing generation in the running Marketplace operator configuration differs from the indexing generation of its last successful index, when the registry's periodic reconciliation is due, or when an operator runs `serve reindex` for it, even if commit and indexing generation are unchanged. Periodic reconciliation SHALL occur at least once per reconciliation interval, which defaults to 24 hours. A refresh job SHALL have a bounded number of attempts. A trigger that targets the same commit and indexing generation as a refresh that is still active MUST coalesce onto that refresh instead of creating another. A registry whose last successful index recorded a generation from an earlier generation scheme SHALL be refreshed once for its current indexing generation.

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

#### Scenario: A registry indexed under the earlier generation scheme refreshes once

- **WHEN** a registry whose last successful index recorded a startup-configuration generation is observed at the same commit by a worker using canonical indexing generations, and is then observed again
- **THEN** exactly one refresh runs, it records the canonical indexing generation as successful, and the later observation performs no acquisition

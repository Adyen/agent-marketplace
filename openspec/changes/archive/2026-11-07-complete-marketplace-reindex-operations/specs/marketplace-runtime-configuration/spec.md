## ADDED Requirements

### Requirement: A running serve process reports configuration replacement outcomes

A running `serve` worker process SHALL report each distinct configuration replacement as applied or rejected, in its logs and in its configuration candidate metrics, and SHALL keep serving under the last known-good configuration after a rejection.

#### Scenario: A running serve process applies a replaced configuration

- **WHEN** the configuration file of a running `serve` worker process is atomically replaced with a valid configuration that adds a registry
- **THEN** the process logs `config candidate applied`, its `applied` configuration candidate count increases, and the added registry is indexed without a restart

#### Scenario: A running serve process rejects an invalid replacement

- **WHEN** the configuration file of a running `serve` worker process is replaced with an invalid configuration
- **THEN** the process logs `config candidate rejected`, its `rejected` configuration candidate count increases, it keeps running, and its previously configured registries remain listed and observed

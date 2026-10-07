## ADDED Requirements

### Requirement: Migrated selection workflows retain functional coverage

The regression suite SHALL exercise bulk, tag, and ad-hoc installation and removal after migrating a populated legacy configuration to v4. It MUST prove both the intended selection change and preservation of unrelated selected resources, including a subsequent synchronization and drift check.

#### Scenario: Migrated selection workflows remain usable

- **WHEN** a populated legacy installation migrates to v4 and then installs and removes resources through bulk, tag, or ad-hoc selection
- **THEN** the intended resources MUST materialize and remain removed after subsequent synchronization
- **AND** unrelated selected resources MUST remain installed
- **AND** synchronization checking MUST succeed

### Requirement: Released clients consume candidate committed state

The released-client regression gate SHALL use a checksum-verified pinned stable release in an isolated environment. That released client MUST successfully synchronize locked state and check synchronization after the candidate synchronizes, installs, updates, and removes resources in their shared repository. Plain candidate synchronization of a healthy committed acquisition lock and released-client locked/check reads MUST preserve its bytes.

#### Scenario: Released client remains usable after candidate mutations

- **WHEN** the candidate synchronizes, installs, updates, and removes resources in a repository shared with the pinned stable release
- **THEN** the released client MUST pass locked synchronization and synchronization checking after each mutation
- **AND** the candidate's update and removal MUST change the intended materialized resource
- **AND** healthy committed lock bytes MUST remain unchanged through plain candidate synchronization and released-client locked/check reads

### Requirement: Nightly stress completes its declared repetitions

The nightly real-terminal regression job SHALL run the complete journey suite 20 times, retaining per-journey timing assertions and stopping on failure. Its bounded job deadline MUST accommodate the measured full workload and setup without reducing repetitions or adding retries.

#### Scenario: Nightly budget accommodates all repetitions

- **WHEN** the scheduled or manually dispatched nightly stress job runs the measured 20-iteration workload
- **THEN** its job deadline MUST allow the full workload and setup headroom
- **AND** every iteration MUST run the complete journey suite with failures propagated

# regression-gates Specification

## Purpose
Protect migrated selection workflows, interoperability with the pinned stable release, and complete nightly real-terminal stress execution.

## Requirements

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

### Requirement: Public Cloud analysis preserves CI trust boundaries

Public CI SHALL submit eligible source revisions to SonarQube Cloud without referencing internal analysis infrastructure. Main-branch pushes and same-repository pull requests targeting main SHALL be eligible; manual analysis SHALL be limited to main. Fork and Dependabot pull requests MUST NOT receive analysis credentials. Missing analysis credentials SHALL produce an explicit onboarding skip rather than a fabricated analysis result. Available credentials MUST NOT be supplied to source build or test execution. Existing CI gates and branch-protection requirements MUST remain unchanged.

#### Scenario: Eligible revisions use the public service

- **WHEN** a main-branch push or same-repository pull request targeting main has analysis credentials available
- **THEN** its analysis SHALL use the public SonarQube Cloud service
- **AND** the submitted analysis SHALL identify the analyzed source revision
- **AND** public configuration MUST NOT reference internal analysis endpoints or execution infrastructure

#### Scenario: Untrusted requests receive no analysis credentials

- **WHEN** the request originates from a fork or Dependabot, or manual analysis targets a branch other than main
- **THEN** token-dependent analysis SHALL be skipped
- **AND** no privileged alternative trigger SHALL execute that request's code

#### Scenario: Missing credentials produce an explicit skip

- **WHEN** an otherwise eligible run has no analysis token configured
- **THEN** the run SHALL explain that analysis was skipped pending token setup
- **AND** it MUST NOT invoke the scanner or claim an analysis result

### Requirement: Cloud analysis consumes matching coverage evidence

Coverage evidence submitted with Cloud analysis SHALL be freshly generated from the same revision in the same workflow run, without analysis credentials in the build or test environment. Failed coverage generation, missing reports, and scanner failures MUST remain failures rather than successful analysis. Go coverage SHALL describe the root-module short-mode test run; analysis of frontend source MUST NOT be represented as measured frontend coverage without corresponding reports.

#### Scenario: Analysis imports fresh matching coverage

- **WHEN** an eligible revision completes root-module short-mode Go tests with coverage
- **THEN** analysis SHALL import the report generated for that revision in that workflow run
- **AND** no report from another run or revision SHALL be substituted
- **AND** source tests MUST NOT receive analysis credentials

#### Scenario: Analysis failures remain visible

- **WHEN** coverage generation fails, the required coverage report is missing, or the scanner fails
- **THEN** the affected workflow SHALL not report successful analysis
- **AND** scanner execution SHALL depend on successful coverage generation and receipt of its report

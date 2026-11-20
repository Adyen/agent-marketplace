# Spec Delta

## ADDED Requirements

### Requirement: Select eligible pull-request benchmarks independently
On `pull_request` opened, synchronize, reopened, and labeled events, an eligible PR SHALL start an independent Benchmark workflow when existing change selection marks benchmarks or the `run-benchmarks` label is present. Other PRs SHALL NOT run benchmarks.

#### Scenario: Eligible PR starts independent benchmark workflow
- **GIVEN** a pull request whose changed files qualify for benchmarks
- **WHEN** it is opened, synchronized, reopened, or labeled
- **THEN** an independent Benchmark workflow starts for that PR

#### Scenario: Documentation-only PR skips unless opted in
- **GIVEN** a documentation-only pull request that is not labeled `run-benchmarks`
- **WHEN** the pull request workflow is selected
- **THEN** benchmarks do not run; adding `run-benchmarks` opts the PR into the Benchmark workflow

### Requirement: Keep benchmark comparisons bound and serial
The Benchmark workflow SHALL compare the exact pull-request base and head revisions serially on one runner, preserving the existing benchmark selection, samples, report, artifacts, and scoped permissions.

#### Scenario: Exact base and head are benchmarked serially
- **GIVEN** an eligible pull request with base SHA B and head SHA H
- **WHEN** its Benchmark workflow runs
- **THEN** it benchmarks B and H serially on the same runner and produces the existing report and artifacts

### Requirement: Keep benchmark outcomes outside required CI
Benchmark runs SHALL remain informational: pending or failed benchmarks SHALL NOT gate required CI, change its status names, or determine its success.

#### Scenario: Failed required CI can be retried while benchmark runs
- **GIVEN** a completed failed required CI run and an independent Benchmark workflow still running for the same eligible PR
- **WHEN** required CI is retried
- **THEN** the retry can proceed without waiting for the Benchmark workflow, whose status does not affect required CI

### Requirement: Preserve the fork comment boundary
The Benchmark workflow SHALL keep the existing pull-request permission and same-repository comment guard; fork pull requests SHALL NOT attempt the privileged benchmark comment.

#### Scenario: Fork PR does not attempt a privileged comment
- **GIVEN** an eligible pull request from a fork
- **WHEN** benchmark reporting completes
- **THEN** it records the report and artifacts without attempting to create or update a pull-request comment

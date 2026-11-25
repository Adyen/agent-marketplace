# benchmark-reporting Specification

## Purpose
Provide concise benchmark comparisons that distinguish observed effects from statistical evidence and do not claim a PR caused a performance change.

## Requirements

### Requirement: Significance accounts for multiple comparisons

The benchmark report SHALL apply Holm correction across every comparable benchmark metric in one report before treating a difference as significant. The family MUST include time, bytes, and allocation tests even when unchanged or below the notable threshold, and MUST exclude benchmarks present on only one side. A notable difference MUST satisfy adjusted p<0.05 and an absolute median change of at least 10%. The report SHALL count notable metric increases and decreases separately without classifying them as confirmed regressions or improvements.

#### Scenario: Weak evidence is not promoted
- **WHEN** a report contains differences that meet the unadjusted significance and effect thresholds but fail the report-wide correction
- **THEN** those differences MUST NOT appear as notable changes
- **AND** the report MUST expose their corrected evidence in collapsed details without claiming equivalence

#### Scenario: Strong evidence remains highlighted
- **WHEN** an observed metric increase or decrease passes the corrected significance and effect thresholds
- **THEN** the report MUST include its observed direction and magnitude in the compact notable table
- **AND** added or removed benchmarks MUST remain identified without significance claims

#### Scenario: Mixed metric directions remain visible
- **WHEN** one benchmark has a qualifying time increase and qualifying memory decrease
- **THEN** the report MUST count and show both directions rather than assigning one regression or improvement label

### Requirement: Statistical details do not overwhelm the summary

The report SHALL keep sample counts, adjusted p-values, and statistical method explanations inside collapsed details. The expanded summary MUST include a short runner-conditions caveat. Details MUST state that significance does not establish PR causation and that insufficient evidence does not establish unchanged performance.

#### Scenario: Technical evidence stays collapsed
- **WHEN** a report is rendered
- **THEN** the expanded section MUST show concise direction counts and notable effects without statistical-method or p-value details
- **AND** collapsed details MUST show base/head sample counts, adjusted p-values for each comparable metric, and the evidence limitations

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

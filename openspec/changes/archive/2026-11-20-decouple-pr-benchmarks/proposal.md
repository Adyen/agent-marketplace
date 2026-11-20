# Proposal

## Why

The benchmark job currently shares a pull-request workflow with required CI, so a long-running informational benchmark can delay retrying a completed failed CI run. Separating the workflow keeps benchmark reporting useful without coupling its duration or result to required checks.

## What Changes

- Run eligible pull-request benchmarks in an independent workflow, retaining the existing planner selection and `run-benchmarks` opt-in.
- Keep benchmark results informational and independent of required status names and outcomes.
- Preserve pull-request event types, exact base/head comparison, benchmark selection and methodology, same-runner serial execution, report and artifact behavior, and current fork/comment permissions.
- Do not change E2E coverage, telemetry tests, or use `pull_request_target`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `benchmark-reporting`: specify independent workflow selection and preserve benchmarks as informational, non-required pull-request evidence.

## Impact

GitHub Actions workflow wiring and its CI planner tests; no benchmark metrics, comparison methodology, required checks, or telemetry tests change.

## Release note

Pull-request benchmarks will run independently of required CI, so a completed failed CI run can be retried without waiting for a benchmark. Benchmark results remain informational, and the `run-benchmarks` label continues to opt in.

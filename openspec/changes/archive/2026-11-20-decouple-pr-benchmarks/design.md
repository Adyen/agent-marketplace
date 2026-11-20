# Design

## Context

The existing `benchmark` job is selected by the shared change planner or the `run-benchmarks` label and currently lives in the required CI workflow. Its benchmark execution, report, artifacts, and same-repository comment guard are already established behavior.

## Goals / Non-Goals

**Goals:**
- Run the existing benchmark job in an independent pull-request workflow while reusing the current selection logic.
- Keep benchmark results informational and preserve existing comparisons and reporting.

**Non-Goals:**
- Change required-check identities or CI triggers, E2E coverage, benchmark selection or methodology, telemetry tests, or fork permissions.
- Use `pull_request_target` or grant fork workflows privileged comment access.

## Decisions

- Add a dedicated Benchmark workflow for the existing `pull_request` events: opened, synchronize, reopened, and labeled. Give it its own concurrency group keyed to the PR so benchmark runs do not share cancellation or status dependencies with required CI.
- Reuse `tools/ci-plan` for the same base/head file selection and keep the `run-benchmarks` label as an opt-in. Keep the current CI workflow trigger and required jobs unchanged; remove only the informational benchmark job from that workflow.
- Move the benchmark job's pinned base/head checkouts and remaining steps without changing package selection, sample count, benchmark order, same-runner comparison, report generation, artifacts, or scoped permissions.
- Retain the comment step's same-repository guard. Use ordinary `pull_request` events and current permission behavior; do not attempt privileged comments for forks.
- Add workflow-wiring tests in `tools/ci-plan` and a hosted witness that confirms Benchmark and E2E Gates remain independent.

## Risks / Trade-offs

- [Risk] Duplicated workflow planning could drift from required CI selection → Keep both paths driven by the shared planner and test their selection parity.
- [Risk] A benchmark workflow edit could accidentally become a required gate → Test that it has no dependency from required CI and retain the existing required status names.

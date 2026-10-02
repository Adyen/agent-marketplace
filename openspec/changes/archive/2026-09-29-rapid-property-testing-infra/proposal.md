## Why

The approved Rapid property-testing plan (`docs/plans/2026-09-28-rapid-property-testing-design.md`) needs a shared test-only foundation before the per-property suites land: the `pgregory.net/rapid` dependency, a guard keeping it out of the shipped binary, and repository standards for authoring properties. None of this changes user-observable behavior.

## What Changes

- Adds `pgregory.net/rapid` v1.3.0 as a test-only Go dependency.
- Adds `TestRapidProductionDependencyGuard` in `cmd/agent-marketplace`, which fails if `pgregory.net/rapid` appears in the `go list -deps` production graph, with a `github.com/spf13/cobra` non-vacuity control.
- Ignores Rapid failure files under `testdata/rapid/` and adds property-test authoring standards to `CODING_STANDARDS.md`.

## Capabilities

### New Capabilities

None. This change is test infrastructure only and carries no spec delta (`skip_specs: true`); normative specs describe user-observable behavior only.

### Modified Capabilities

None.

## Impact

- Test and documentation only; no runtime, CLI, configuration, lockfile, materialization, or wire-format behavior changes.

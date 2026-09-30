## Context

See proposal.md — Why. The full technical evaluation lives in `docs/plans/2026-09-28-rapid-property-testing-design.md` (approved design); this change implements only its Phase 0 shared infrastructure. Constraint from spec-governance: this layer touches no user-observable behavior, so the change carries `skip_specs: true` and no spec delta.

## Goals / Non-Goals

**Goals:**
- Land `pgregory.net/rapid` v1.3.0 as a test-only dependency usable by later per-property suites.
- Prove mechanically that Rapid can never link into the shipped binary.
- Record repository standards for property-test authoring so later layers write consistent properties.

**Non-Goals:**
- No property tests themselves; targets A (ownership), B (lockfile codec), C, F land in their own changes.
- No CI/workflow changes; properties run inside the existing `make test-witnessed` job.
- No shared generator package; each later target generates unrelated types beside its own tests.

## Decisions

### D1. `pgregory.net/rapid` over alternatives

Adopted per the approved plan: automatic shrinking to readable counterexamples, generics-native API, stdlib-only dependency tree, MPL-2.0 with test-only usage. Rejected alternatives recorded in the plan: `hegel.dev/go/hegel` (beta API, native `libhegel` cache extraction conflicts with test-machine isolation), `testing/quick` (frozen, no shrinking), `leanovate/gopter` (manual generator ceremony), native Go fuzzing (kept for raw-byte parser boundaries only).

### D2. Test-only enforcement via a dependency-graph guard, not convention

Rapid registers `-rapid.*` flags in `init`, so linking it into production is user-visible. Convention alone cannot prevent that, so `TestRapidProductionDependencyGuard` runs `go list -deps ./cmd/agent-marketplace` and fails on `pgregory.net/rapid`. A `github.com/spf13/cobra` non-vacuity control proves the listing is real, so an empty or failed `go list` cannot pass silently.

### D3. Failure files ignored, counterexamples promoted

`testdata/rapid/` is gitignored: Rapid's minimized failfiles are ephemeral diagnostics. A real counterexample is converted into a plain example regression test in the same change as its fix, keeping the committed suite self-contained.

### D4. Rapid's built-in check counts, no helper

Default 100 checks per `rapid.Check`, divided by 5 under `go test -short`; `RAPID_CHECKS` stays a local override. No repository wrapper is added because Rapid exposes no per-test count.

## Risks / Trade-offs

- Guard test depends on the local Go toolchain → it already runs inside `go test`, which requires the toolchain; `GOMODCACHE` is forwarded so module listing works in restricted environments.
- MPL-2.0 test-only dependency requires license approval → recorded as a merge precondition in the approved plan; test-only linking avoids distribution obligations.
- `skip_specs: true` means no `// spec:` traceability for this layer → acceptable because specaudit gate reports no behavior-bearing changes; the guard behavior itself is asserted by its own test.

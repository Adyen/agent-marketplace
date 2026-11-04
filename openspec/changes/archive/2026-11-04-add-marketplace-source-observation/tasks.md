# Tasks

## 1. Remote ref resolution

- [x] 1.1 Add local bare-repository tests for branch tip, annotated and lightweight tags, full commit without a Git process, ambiguous short ref, missing ref, and credential-free errors and logs, each scenario bound by `// spec: marketplace-source-observation/<scenario-slug>`; verify they fail on the base commit
- [x] 1.2 Implement remote ref resolution and source host identity on the existing acquisition Git path; verify the resolution tests pass

## 2. Observation scheduler

- [x] 2.1 Add fake-clock tests for stable phases across restarts, deterministic capped backoff, and visible host-budget delay, bound to their scenarios; verify they fail on the base commit
- [x] 2.2 Add worker integration tests against local fixture repositories counting probes and acquisitions for unchanged sources, missing refs, changed commits, manual reindex, due reconciliation, coalesced triggers, two workers sharing one store, the host budget across workers, and secret-free telemetry, bound to their scenarios; verify they fail on the base commit
- [x] 2.3 Implement the observation loop, conditional refresh requests, refresh-job bookkeeping, and manual reindex in operator-config management mode; verify every scenario test and the existing worker tests pass

## 3. Documentation

- [x] 3.1 Update the operator configuration reference, the reindex-and-monitor how-to, and the `serve reindex` CLI reference to describe observation-driven refreshes; verify the described defaults match the code

## 4. Integration

- [x] 4.1 Run `make spec-check`, `make test TEST_PACKAGES="./pkg/marketplace/..." TEST_FLAGS="-count=1"`, and `make validate`; verify all pass
- [x] 4.2 Run `openspec archive add-marketplace-source-observation --yes` as the final commit and verify `make spec-check` still passes

## 1. Baseline Evidence

- [x] 1.1 Capture the existing `Resolve` and materialization hot-path benchmarks at the approved base, retaining raw output and invocation details; verify both benchmark results are recorded without imposing a performance threshold.

## 2. Configuration Setting

- [x] 2.1 Add `RequiresConfig` with `Expand *bool` and `RequiresExpansionEnabled()` plus the v4 `requires.expand` setting, explicit-false preservation, existing layered and recursive-include precedence, false default, and strict validation; verify focused config tests cover true/false, absence, precedence, includes, and invalid values.
- [x] 2.2 Keep v2/v3 expansion disabled by default and ensure migration does not seed `requires.expand`; verify legacy-read, migration, and settings-with-unreadable-acquisition-lock tests.

## 3. Resolver Expansion

- [x] 3.1 Add `WithImpliedPreview(*[]ResolvedArtifact)`, `ResourceRef{Kind ir.ResourceKind, Name string}`, `ResolvedArtifact.RequiredBy`, and `ResolveResult.ImpliedPreview`, retaining the existing `Resolve` signature and collection-only `Via`; verify resolver consumers compile and focused tests pass.
- [x] 3.2 Implement post-collection, post-required-shadow prerequisite lookup and transitive expansion from already-discovered configured catalogs, including exact unhinted source identity, configured Git hints with root narrowing, absolute cleaned directory hints, and no fallback or acquisition; verify table tests cover lookup outcomes and malformed-candidate diagnostics.
- [x] 3.3 Apply per-scope and effective harness-target admission, explicit and exclude-all precedence, required-selection exceptions, direct-selection winners, deterministic declaration/resolution ordering, and cycle termination; verify resolver cases cover scope collisions, targets, exclusions, required selections, direct winners, and cycles.
- [x] 3.4 Keep disabled expansion's implied closure separate from actual selections, and add typed unmet reasons while preserving warning text, remediation, and strict-sync semantics; verify advisory, enabled, typed-reason, and strict-sync tests pass with unchanged selections when disabled.
- [x] 3.5 Add resolver property tests for idempotent closure, no explicitly excluded non-required selection, provenance reachability, and disabled-mode selection equivalence; verify the property suite passes at its existing stress settings.

## 4. Materialization Preservation

- [x] 4.1 Add `WithImpliedPreview` as a preservation-only materialization input and compile previews separately from desired writes; preserve only exact existing managed file/link/manifest paths and aggregate identities with matching ownership at each admitted scope and harness target; verify file/link/manifest, shared JSON, and Codex TOML ownership tests cover no creation, rewrite, overwrite, or handwritten adoption.
- [x] 4.2 Preserve unrelated stale-output pruning and existing fail-closed behavior, and make plain sync, `sync --locked`, and `sync --check` tolerate existing preview outputs without changing healthy acquisition-lock bytes; verify ownership and command regressions cover unrelated pruning, missing/corrupt ownership, malformed aggregates, lock-byte identity, and cleanup after the last requiring selection disappears.

## 5. Removal and End-to-End Coverage

- [x] 5.1 Update authorized removal of a still-required optional resource to exclude it only when expansion is enabled, retain requiring resources and report their unmet prerequisites, and preserve existing required-policy and scope authorization; verify advisory/enabled remove command tests cover direct-selection removal, effective exclusion, warning behavior, and no cascade.
- [x] 5.2 Add declarative local-Git E2E cases for same-source requirements, a configured cross-source hint, and an unconfigured hint; verify each case exercises resolution and the expected warning/selection result.
- [x] 5.3 Bind every new or modified spec scenario to a test annotation using its exact capability path and scenario slug; verify `go run ./tools/specaudit trace` reports no unbound or dangling scenarios.

## 6. Documentation, Performance, and Gates

- [x] 6.1 Update the relevant reference, how-to, and glossary pages for default-false opt-in and advisory preview preservation without claiming Marketplace lookup or an N+1 default flip; verify the existing documentation checks accept the updates.
- [ ] 6.2 Repeat the existing `Resolve` and materialization hot-path benchmarks and retain raw output and invocation details alongside the baseline; report observed deltas without introducing a numeric budget.
- [ ] 6.3 Run `make validate`, `make spec-check`, `make validate-e2e`, and `openspec validate --all --strict`; verify each gate succeeds, including selected TUI consumer regressions without adding a new TUI journey.

## 7. Archive

- [x] 7.1 Run `openspec archive expand-resource-requirements --yes` as the final change task and verify the archived specs match the validated delta.

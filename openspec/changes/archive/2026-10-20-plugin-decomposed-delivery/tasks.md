## 1. Support-file assembly

- [x] 1.1 Port PR #273's `pkg/harness-compilation` changes (design D5),
  dropping the `convert` hunks, and add the three library-level
  `Convert` + `Materialize` regressions

## 2. Package generations and decomposed delivery

- [x] 2.1 Publish package generations atomically under the state root,
  tracked in ownership state, as GC roots (design D1). Point plugin-root
  resolution at generations. Annotate the generation scenarios
- [x] 2.2 Deliver decomposed members to all seven harness targets with the
  namespace in design D2, from the per-layer post-exclusion member keys;
  keep `delivery: native` pending (design D4); annotate the namespace
  scenarios
- [x] 2.3 Implement the plugin instance acceptance rule as one transaction
  participant per instance (design D3); annotate its scenarios, including
  an `AGENT_MARKETPLACE_E2E_CRASH_AT` interruption test with no staging
  residue
- [x] 2.4 Implement removal and plugin-data retention; annotate the removal
  scenarios

## 3. Gates

- [x] 3.1 Run `make validate`, `make spec-check`, and `make validate-e2e`

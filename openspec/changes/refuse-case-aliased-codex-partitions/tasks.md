## Implementation

- [x] Add failing regressions for a case-variant CODEX_HOME with conflicting and disjoint selections, a Unicode-normalization alias, and a disjoint case-variant root control; annotate each scenario.
- [x] Compare Codex managed paths of different partitions by case-folded physical location before mutation.
- [x] Prove the case-folding and identity checks with negative mutations, and run the frozen case-alias reproduction and Codex tests on a case-insensitive volume.
- [x] Verify scoped materialization tests, lint, build and strict specs.
- [x] Adversarially review every changed entry.
- [ ] `openspec archive refuse-case-aliased-codex-partitions --yes`

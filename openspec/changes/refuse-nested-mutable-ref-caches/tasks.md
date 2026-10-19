## Implementation

- [x] Add failing regressions for a nested ref inside a cached checkout (present and absent nested paths), a cache directory without its own repository under an enclosing HOME repository, and sibling slash-ref controls; annotate each scenario.
- [x] Refuse nested and non-checkout cache directories before any Git operation or directory creation.
- [x] Prove each check with a negative mutation and rerun the frozen nested-ref reproduction.
- [x] Verify scoped tests of mutable acquisition consumers, lint, build and strict specs.
- [x] Adversarially review every changed entry.
- [ ] `openspec archive refuse-nested-mutable-ref-caches --yes`

## 1. Prove reporting behavior

- [x] 1.1 Add sandboxed TestSurfaceRequirements command tests for every new or modified delta scenario with spec annotations and positive/negative output controls; observe focused failure on the unchanged base.
- [x] 1.2 Prove direct RequiredBy providers are not called implied, transitive install reachability/dedup/filtering, collection behavior, hint redaction and warning-only doctor/read-only safety.

## 2. Implement CLI relationship reporting

- [x] 2.1 Carry reporting-only implied-admission metadata and capture existing sync resolution/preview into install results without changing selection or adding acquisition.
- [x] 2.2 Render install after-summary relationship output, configured-list VIA, ad-hoc REQUIRES, status VIA/REASON and doctor unconfigured-source warning.
- [x] 2.3 Run focused command and reporting-seam regressions, retaining strict exits and existing command witnesses.

## 3. Reconcile documentation and verification

- [x] 3.1 Reconcile affected CLI/prerequisite reference, how-to and glossary claims with tested release-N behavior.
- [x] 3.2 Capture matched CLI before/after evidence and repository-required same-workload list/status benchmark evidence without invented performance acceptance claims.
- [ ] 3.3 Run scoped native validation, strict spec audit and combined E2E verification on the clean candidate.
- [x] 3.4 Run `openspec archive surface-requirements-cli --yes` in the same pull request as the implementation.

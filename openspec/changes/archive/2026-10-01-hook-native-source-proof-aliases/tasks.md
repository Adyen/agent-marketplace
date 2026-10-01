## 1. Contributor-aware ownership evidence

- [x] 1.1 Add optional validated hook source-evidence tuples and exact proof matching while preserving scalar fields and the state version. Verify ownership roundtrip, absent/scalar compatibility, wrong types, empty values, non-hook evidence, and duplicate/case-alias refusal tests pass.
- [x] 1.2 Use contributor matching within the existing complete current-byte proof loop. Annotate tests for `several-selected-sources-share-one-clean-native-entry`, `scalar-source-evidence-remains-compatible`, `unproven-contributor-retains-legacy-behavior`, and `partial-shared-proof-cannot-suppress-a-selection`. Verify focused materialization tests pass for all four tolerance targets in both scopes with native/legacy/state preservation controls.

## 2. Consumer and safety proof

- [x] 2.1 Add isolated sync, locked-sync, and check consumer tests with two selected resources sharing one proven native entry, plus partial and corrupt evidence controls. Annotate `invalid-additional-evidence-refuses-without-mutation` and `read-only-shared-proof-checks-leave-state-untouched`. Verify actual command success/refusal codes and unchanged bytes, mtimes, permissions, and absent legacy duplicates.
- [x] 2.2 Retain unchanged source/mirror fingerprint vectors and record the native tolerance and existing ownership-load benchmark before/after results using identical parent-authored inputs. Verify focused tests and benchmark artifacts are present without payload leakage.

## 3. Delivery

- [ ] 3.1 Run focused tests, documentation reconciliation, verification maintenance when mapped behavior changes, unslop, native/domain gates, and fresh independent reviews on the clean candidate. Verify the reviewed update to the existing pull request has green CI; merging and release remain human actions.
- [x] 3.2 Run `openspec archive hook-native-source-proof-aliases --yes` as the final implementation commit of the same pull request, and verify `make spec-check` passes with the synced scenarios bound to tests.

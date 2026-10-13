## Implementation

- [x] Capture five-sample before measurements using the existing actual native alias disk-I/O benchmark.
- [x] Reproduce concurrent destination loss through actual native synchronization for both nonempty and empty alias targets before fixing the boundary.
- [x] Retain captured target, logical alias and parent authority through descriptor-relative detachment and generation-checked removal, preserving existing through-link publication semantics.
- [x] Add scenario-bound native generation, foreign-staging and empty/private/no-op controls with actual healthy retries and unchanged target preservation.

## Verification and delivery

- [x] Verify affected native and shared consumers, alias admission/compatibility, typed conflicts, staging cleanup and original/concurrent metadata preservation on Linux/macOS and ten repeated races. The adversarial source-directory alias retargeting variant failed before the added original-resolution comparison and passes with real healthy retry afterward.
- [x] Measure after-cost without weakening safety. Warmed five-sample, two-second host measurements on pre-fix 6940ad3c and the final follow-up measured median 4.470ms to 10.363ms (+131.85%), bytes/op +9.18% and allocations 4924 to 5394 (+9.55%), each p=0.008. This includes actual native filesystem I/O and alias recreation, with no network. The earlier after-samples overlapped heavy gates and are not used for the reported comparison. The safety cost is confined to alias migration; no measured regular-file steady-state performance claim is made.
- [x] Run scoped make validation for fsboundary, materialization, SDK, config, dependency transactions, ownership, backup and pipeline, including fresh selected short-mode tests, vet, zero-issue lint and build. CLI conversion/deinit regressions and strict specifications passed (559 scenarios / 888 annotations). Final combined E2E/configuration compatibility passed in 558.317s, followed by shellcheck; historical executable and vendor-native gates remain separate.
- [x] Complete host-driven adversarial OCR-delegate review of all ten pre-archive follow-up files, including all six default exclusions and consumer reachability, with no skips or remaining actionable findings. Validate failure before writes, original target/logical alias/parent and resolution authority, staging ownership/cleanup, private/shared modes and non-vacuous native/CLI retries. The reproduced source-parent resolution gap found during review is fixed and regression-tested.
- [ ] Run `openspec archive retain-native-marker-alias-generations --yes` and post-archive spec checks in the same pull request.

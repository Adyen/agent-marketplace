## Implementation

- [x] Add annotated failing public conversion matrices for native, empty, and absent subagent tool policies, with supported-mapping, explicit-override, and foreign-identifier controls.
- [x] Add annotated CLI conversion and native install/sync tests.
- [x] Preserve Factory, Cursor, and Gemini native tool lists in typed sidecars and share native-first tool resolution in their compilers.

## Verification

- [x] Run focused compiler and consumer regressions and compare tool-policy benchmarks.
- [x] Run `make validate` and `make validate-e2e`.
- [ ] Run `make spec-check` after archiving the change.
- [x] Review the full intended change adversarially and fix verified findings.
- [ ] `openspec archive preserve-native-subagent-tool-policies --yes`

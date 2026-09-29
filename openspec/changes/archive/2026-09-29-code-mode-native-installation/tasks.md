## 1. Test-first contract

- [x] 1.1 Add or update annotated command and E2E tests for collection install, default-off behavior, unavailable executable and unsupported target, proving the new test fails on the base commit and passes with the new registry resources.
- [x] 1.2 Add or update annotated ownership tests for normal collection removal, independent selections, handwritten collisions, and repeated sync; verify through the real install/remove pipeline.
- [x] 1.3 Add or update annotated config/CLI tests for rejecting old codeMode roots and fragments without writes, removal of retired commands and flag, and preserving stdio search/execute; verify the focused tests fail on the base for the intended reason.

## 2. Native distribution and migration

- [x] 2.1 Add first-party Git-registry code-mode collection, bootstrap skill, and MCP definition; verify normal registry discovery and materialization on selected harness targets with a real executable.
- [x] 2.2 Remove the synthetic code-mode sync entry and special CLI/config enablement; verify legacy config rejection and unchanged files, and ordinary ownership-safe install/remove through focused tests.
- [x] 2.3 Reconcile command witnesses and host E2E tests for retired flags and the new journey; verify focused package and host E2E suites pass.

## 3. Documentation and verification

- [x] 3.1 Update code-mode tutorial, CLI and configuration reference for collection installation and migration; verify site build and focused link/CLI checks.
- [x] 3.2 Run make spec-check and make validate-full on the candidate; verify all affected requirements have annotated passing test witnesses.
- [x] 3.3 Run `openspec archive code-mode-native-installation --yes` as the final change step in the same pull request and verify make spec-check passes on the archived candidate.

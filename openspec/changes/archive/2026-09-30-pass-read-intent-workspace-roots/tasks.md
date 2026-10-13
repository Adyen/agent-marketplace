## 1. Prove root selection

- [x] 1.1 Add failing config test for empty repository root with no layer reads; verify focused test fails on base for the missing error.
- [x] 1.2 Add nested-directory tests for list --from, search, doctor, and GC, with `// spec: workspace-resolution/nested-personal-local-browsing-and-inspection`; verify they expose both selected roots.
- [x] 1.3 Add escaping-root tests for the affected consumers with `// spec: workspace-resolution/escaping-personal-local-root-during-browsing-and-collection`; verify failure precedes reads, acquisition, and writes.

## 2. Wire validated roots

- [x] 2.1 Remove root discovery from config.ReadIntent and reject empty repository root; verify focused config tests pass.
- [x] 2.2 Construct and validate Workspace at production CLI and pipeline call sites before passing both roots to configuration reads; verify focused command and pipeline tests pass.

## 3. Verify and publish specifications

- [x] 3.1 Run make test, make ci-lint, make build, make spec-check, and relevant E2E tests; verify successful exits and all added spec scenarios have test witnesses.
- [x] 3.2 Run `openspec archive pass-read-intent-workspace-roots --yes` in the same pull request; verify the main workspace-resolution spec contains both added scenarios.

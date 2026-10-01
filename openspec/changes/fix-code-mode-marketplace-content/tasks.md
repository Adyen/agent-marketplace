## 1. Regression evidence

- [x] 1.1 Add annotated local HTTP and isolated-cache regressions for exact metadata refresh, canonical support and legacy asset access, offline reuse, and refusal of stale fallback; verify intended focused failures before implementation and on the base commit.

## 2. Marketplace content loader

- [x] 2.1 Refresh exact metadata online, retain verified-cache-only offline access, enforce fresh availability and expected artifact digests; verify focused regression tests pass.
- [x] 2.2 Expose the sorted bounded canonical support and legacy asset namespace with ambiguous-path refusal and existing path/byte limits; verify script load/read regressions pass.

## 3. Verification and publication claims

- [ ] 3.1 Reconcile directly affected code-mode documentation and measure the affected load path; verify claims match tested behavior and retain before/after benchmark evidence.
- [ ] 3.2 Run affected-package and code-mode E2E checks, full native validation, strict OpenSpec validation, and scenario tracing; retain passing evidence on the final candidate.
- [ ] 3.3 Run `openspec archive fix-code-mode-marketplace-content --yes` in the same pull request and verify the synchronized main specification and archive.

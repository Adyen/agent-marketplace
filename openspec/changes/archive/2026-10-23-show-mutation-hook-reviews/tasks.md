## 1. Pipeline and CLI

- [x] 1.1 Add failing command regressions for install, update, removal, unchanged runs, materialization warnings, Marketplace/collection installs, and partial failures with scenario annotations.
- [x] 1.2 Preserve sync reports and materialization warnings through all mutation results and reuse the hook review formatter in CLI output.

## 2. TUI

- [x] 2.1 Add failing TUI regressions for lifecycle and Marketplace completion, retained reviews and warnings, no-op clearing, terminal escaping, and sync-and-quit.
- [x] 2.2 Present completed reviews in the existing diagnostics viewport after authoritative reload and preserve operation warnings.
- [x] 2.3 Add and document real-terminal journeys for hook additions, updates, removals, reopening, no-op clearing, and warning-only sync-and-quit.

## 3. Verification and review

- [x] 3.1 Run focused tests, relevant benchmarks, make validate, make spec-check, make validate-e2e, and make tui-qa.
- [x] 3.2 Privacy-screen the exact change, run OCR and adversarial review, address confirmed findings, and record review coverage.
- [x] 3.3 Run openspec archive show-mutation-hook-reviews --yes as the final implementation commit.

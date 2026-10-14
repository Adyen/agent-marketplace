## 1. Regression evidence

- [x] Reproduce unsafe blob addresses, active support schemes, raw log attributes, and native integer overflow through consumer tests.
- [x] Add proxy, publication, and descriptor-read controls.

## 2. Boundary fixes

- [x] Contain blob operations and archive paths; complete inspection scheme checks.
- [x] Check native integer boundaries and remove unchecked allocation hints.
- [x] Encode log attributes and anchor JavaScript reads/proxy origins.

## 3. Verification

- [x] Run focused package and JavaScript tests plus before/after benchmarks.
- [x] Run make validate, spec-check, validate-e2e, and TUI QA.
- [x] Review every changed file using OCR delegation and validate findings.
- [x] Archive with openspec archive harden-codeql-boundaries --yes.

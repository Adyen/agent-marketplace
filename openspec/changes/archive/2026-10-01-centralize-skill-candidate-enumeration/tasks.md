## 1. Shared skill enumeration

- [x] 1.1 Capture existing ingestion benchmark baselines and add helper tests for literal `.git` directory/file/symlink entries, valid and near-match controls, ordering, and read-error identity.
- [x] 1.2 Implement shared `ingest.ReadSkillEntries` and route the six affected source layouts through it without changing their parsing, diagnostics, or symlink policy.
- [x] 1.3 Verify direct ingestion, catalog, CLI/TUI regressions and ordinary Factory skill symlinks; run matching after benchmarks.

## 2. Verification and archive

- [x] 2.1 Run native validation, spec checks, E2E/configuration compatibility, and TUI QA on the final candidate.
- [x] 2.2 Run `openspec archive centralize-skill-candidate-enumeration --yes --skip-specs` in the same pull request.

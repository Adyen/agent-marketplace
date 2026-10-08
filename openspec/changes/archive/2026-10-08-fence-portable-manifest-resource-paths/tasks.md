## Implementation

- [x] Reproduce root-file, in-component aggregate, component-directory and Codex shared-partition deletion through public materialization with independently authored digests and genuine native installation/pruning controls for every harness target and scope.
- [x] Bind both specification scenarios and cover mutating and read-only runs, case variants of in-component aggregate files, faithful metadata/evidence preservation and the established diagnostic. Add a declarative CLI E2E case for sync checking, deinit/purge refusal and genuine repaired teardown.
- [x] Replace the repository Windsurf check with one shared manifest-admission rule, deriving Codex partition components and every harness target's aggregate files from target metadata and preserving error identity and compatibility.

## Verification and delivery

- [x] Record before/after benchmark cost of the portable prune and file-mode paths without weakening safety controls. Ten interleaved runs of prebuilt before/after binaries showed no significant change: Windsurf portable prune 11.37 ms to 11.19 ms (p=0.481), file-mode portable 22.34 ms to 22.71 ms (p=0.853), bytes/operation unchanged, and allocations 6209/op and 7913 to 7914/op.
- [x] Run affected materialization consumers, scoped CLI deinit/conversion regressions, `make validate` for the affected packages, fresh-cache lint, `make spec-check` and `make validate-e2e`. File-mode inventories at v3.1.1, v4.0.0 and the current head stayed admissible. Reverting the fix made every new unit nomination and the new E2E case fail. The combined E2E/configuration gate passed in 703.232 seconds with shellcheck.
- [x] Complete an adversarial review of every changed file, including default-excluded tests and specification artifacts. The host review found the case-variant aggregate alias, which is fixed and covered. The independent OCR-delegate security review covered all eight pre-archive files with no skipped files or remaining actionable findings.
- [x] Run `openspec archive fence-portable-manifest-resource-paths --yes` and verify the archived delta with `make spec-check`.

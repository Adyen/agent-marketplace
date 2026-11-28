# Tasks

## 1. Import validation and normalization

- [x] 1.1 Implement strict parsing for the exact `target-import/v1` manifest and validate identity using the base target/package name grammar, relative paths, and `into`/`support_files` destinations; verify invalid identities, duplicate/unknown YAML, profile mismatch, traversal, symlink, special-file, duplicate-mapping, shipped-name, and missing packaged-dependency cases with focused tests.
- [x] 1.2 Convert accepted mappings to the existing in-memory package representation and sidecar fields without writing a sidecar; verify byte-identical payload and parity with the existing normalized package reader in focused package tests.

## 2. CLI report and destination preview

- [x] 2.1 Add `target dry-run` with the settled flags and JSON report, and annotate CLI tests for every `target-profile-import` scenario; verify valid/refused status, explicit `excluded_files` reasons, diagnostics reserved for problems, nonzero refusal, advisory requirements, pending manual setup, and no runtime-success claim with focused command tests.
- [x] 2.2 Add the optional existing-destination preview using place collision and ownership rules; verify predicted claims, omission of unchanged owned paths, and empty `ownership_changes` when not requested or when the whole package refuses, while destination state remains read-only.
- [x] 2.3 Add command-guard witnesses for the public command and flags, plus an annotated E2E parity case; use sandboxed inputs, network/lock-acquisition counters, and before/after byte, mode, and absence checks for profile/import/source inputs, target files, consumer configuration, locks, approval, ownership, cache, registry, and application state on success and refusal. Verify the public-command guard, the focused E2E case, and `TestTableSpecTags`.

## 3. Delivery checks

- [x] 3.1 Run `openspec validate target-profile-import-dry-run --strict` and `make spec-check`; verify strict validation and all OpenSpec trace/archive checks pass.
- [x] 3.2 Run `make validate`; verify selected tests, non-mutating lint, and build pass.
- [x] 3.3 Run `make validate-e2e`; verify the CLI behavior and side-effect witnesses execute and pass.

## Workflow follow-up

- At delivery, after review and the checks above, run `openspec archive target-profile-import-dry-run --yes` as the final commit of the implementation pull request. Do not archive during planning.

## 1. Annotate proving tests

- [x] 1.1 Annotate `pkg/harness-compilation/model_policy_test.go` for `unresolved-model-warns-and-keeps-the-resource`, `supported-reasoning-emits-native-encoding`, and `same-harness-native-model-strings-round-trip-unchanged`; verify the annotated tests pass
- [x] 1.2 Annotate `pkg/harness-compilation/modelpolicy/resolve_test.go` for `resolved-model-emits-the-native-spelling`, `no-cross-provider-approximation`, `floating-family-selector-pinned-with-notice`, `unsupported-reasoning-warns-without-suppressing`, `conflicting-reasoning-preference-warns-and-resolves-once`, `omitted-model-uses-the-surface-s-native-semantics`, `crossing-inheritance-and-default-boundaries-warns`, `reasoning-on-inherited-selection-is-omitted-when-unrepresentable`, `every-registered-harness-target-ships-a-catalog`, and `malformed-catalogs-are-rejected`; verify `go test ./pkg/harness-compilation/modelpolicy/` passes
- [x] 1.3 Annotate `pkg/harness-compilation/modelpolicy/fixtures_test.go` for `conversion-fixtures-prove-catalog-mappings`; verify the test passes
- [x] 1.4 Annotate `pkg/harness-compilation/model_policy_provenance_test.go` for `ingestion-records-model-source-provenance`; verify the test passes
- [x] 1.5 Annotate `pkg/harness-compilation/model_policy_archive_test.go` for `authoritative-fields-override-stale-legacy-sidecars`, `extras-cannot-forge-provenance-or-resurrect-dropped-fields`, `ambiguous-or-malformed-provenance-is-rejected`, and `legacy-archives-remain-byte-stable-and-resolvable`; verify the tests pass
- [x] 1.6 Annotate `internal/sourcerevision/model_provenance_test.go` for `legacy-source-revisions-ignore-provenance` and `current-source-revision-retains-provenance`; verify the tests pass
- [x] 1.7 Annotate `internal/materialize/model_policy_test.go` for `model-warnings-keep-materialized-resources-converged`; verify the test passes

## 2. Verify and archive

- [x] 2.1 Run `openspec validate baseline-harness-model-catalogs --strict` and `go run ./tools/specaudit trace`; verify both pass
- [x] 2.2 Run `openspec archive baseline-harness-model-catalogs --yes` as the final step of this pull request; verify `openspec validate --all --strict` and `go run ./tools/specaudit trace` pass after archiving

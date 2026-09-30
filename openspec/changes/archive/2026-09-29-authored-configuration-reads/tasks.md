## 1. Authored configuration reads

- [x] 1.1 Use highest-precedence authored allowlist without binding unrelated sources; verify `TestListFromHonorsAllowlistFromUnbindableV4Layer` passes.
- [x] 1.2 Use authored selection labels without remote acquisition; verify `TestConfiguredSelectionLabelsReadsIntentWithoutLocks` passes.

## 2. Bind and archive

- [x] 2.1 Annotate both authored-configuration-reads scenario tests; verify `go run ./tools/specaudit trace` passes.
- [x] 2.2 Run strict OpenSpec validation and focused command tests; verify both commands pass.
- [x] 2.3 Run `openspec archive authored-configuration-reads --yes` as the final change in this pull request; verify the archived spec is present.

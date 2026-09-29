## 1. Separate reads and binding

- [x] 1.1 Read and validate authored configuration before binding remote resources; verify `TestReadLayerIntentV4CapturesReadsAtParse` passes.
- [x] 1.2 Preserve included source identity across later changes; verify `TestReadLayerIntentV4OverlayIncludeRead` and `TestReadLayerIntentV4SymlinkRetargetKeepsCapturedProvenance` pass.

## 2. Bind the spec

- [x] 2.1 Add `// spec:` annotations to the tests proving both configuration-intent scenarios; verify `go run ./tools/specaudit trace` passes.
- [x] 2.2 Run strict OpenSpec validation and focused config tests; verify both commands pass.
- [x] 2.3 Run `openspec archive config-intent-separation --yes` as the final change in this pull request; verify the archived spec is present.

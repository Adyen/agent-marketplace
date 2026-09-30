## 1. Pin lock boundaries

- [x] 1.1 Inventory existing config and pipeline decoder differences, add a `// spec: deployed-acquisition-locks/wrong-scope-in-single-file-read` annotated loader test with a valid control, and verify the shared-reader tests fail on base for the missing reader while existing wrong-scope diagnostics remain stable.
- [x] 1.2 Pin overlay precedence, invalid/unsafe errors, generation digest, and v1 warning behavior with focused package tests; verify each relevant case exercises its actual caller.

## 2. Unify decoding

- [x] 2.1 Add the bounded shared layer reader, remove independent decoders, and run focused config, pipeline, and dependency-transaction tests until green.
- [x] 2.2 Run unchanged lock, GC, and sync-check command tests and confirm their stable error codes; compare combined lines of intent.go, acquisition.go, and the shared reader against base.

## 3. Verify and publish

- [x] 3.1 Run make test, make ci-lint, make build, make spec-check and the affected E2E gate; verify spec witnesses and clean output.
- [ ] 3.2 Run openspec archive unify-layer-lock-reader --yes in the same pull request, then verify strict OpenSpec validation.

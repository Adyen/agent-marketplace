## 1. Pin lock boundaries

- [ ] 1.1 Inventory existing config and pipeline decoder differences and add a red, `// spec: deployed-acquisition-locks/wrong-scope-in-single-file-read` annotated loader test with a valid control; verify it fails on base for accepted wrong-scope evidence.
- [ ] 1.2 Pin overlay precedence, invalid/unsafe errors, generation digest, and v1 warning behavior with focused package tests; verify each relevant case exercises its actual caller.

## 2. Unify decoding

- [ ] 2.1 Add the bounded shared layer reader, remove independent decoders, and run focused config, pipeline, and lockfile tests until green.
- [ ] 2.2 Run unchanged lock, GC, and sync-check command tests and confirm their stable error codes; compare combined lines of intent.go, acquisition.go, and new reader against base.

## 3. Verify and publish

- [ ] 3.1 Run make test, make ci-lint, make build, make spec-check and the affected E2E gate; verify spec witnesses and clean output.
- [ ] 3.2 Run openspec archive unify-layer-lock-reader --yes in the same pull request, then verify strict OpenSpec validation.

## 1. Catalog and resolution

- [x] 1.1 Declare Devin `PreCompact` and `PostCompact` command events with the lifecycle matcher, and mark `PostCompaction` `ingest_only` in the Devin hook catalog.
- [x] 1.2 Validate `ingest_only` events and reject duplicate plain canonical events in the catalog loader.
- [x] 1.3 Skip `ingest_only` events during target event selection.

## 2. Vocabulary

- [x] 2.1 Update Devin hint text in diagnostic codes, the diagnostic catalog, the Devin ingest hint, the field matrix, and the Devin native probe's unrun-probe text.
- [x] 2.2 Regenerate the hook-and-tool-names reference with `make catalog-docs`.

## 3. Verification

- [x] 3.1 Cover canonical compaction compilation, same-harness `PostCompaction`, Devin compaction ingest, and catalog validation with `// spec:` annotated tests.
- [x] 3.2 Run `go test ./pkg/harness-compilation/...`, `go test ./internal/devinprobe ./internal/sourcerevision`, and `make spec-check`.
- [x] 3.3 Run `make validate` once before handoff.
- [ ] 3.4 Archive with `openspec archive devin-compaction-hooks --yes` as the final commit of the pull request (lead).

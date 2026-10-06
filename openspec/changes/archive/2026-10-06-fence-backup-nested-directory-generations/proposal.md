## Why

An admitted selected source directory does not bind child directories that the standard path walker reopens after classification. Replacing a nested directory at descent supplies foreign payloads to a successful archive; actual public backup recovery reproduces imported foreign bytes.

## What Changes

- Reuse existing archiveDir recursively for each classified child directory, retaining original child identity and parent descriptors through member enumeration, payload reads and named-generation validation.
- Use standard descriptor-rooted ReadDir and carry original archive path prefix separately from local source names, preserving lexical ordering, root-dot omission, modes and symbolic metadata without following targets.
- Preserve original/foreign nested generations and creator-only archive cleanup on refusal, with genuine archive recovery and all prior selected-directory controls.

## Capabilities

### Modified Capabilities

- `backup-recovery`: nested directory descent stays on admitted child object generations.

## Impact

R2 child of #571 in stack #560. Existing shared archiveDir/readers/captured directory primitives reused; no new helper/type/public API/dependency/budget/source-comment change. Descriptors stay held per active recursion depth and descriptor exhaustion propagates as refusal, not success; no arbitrary new depth/work budget. Initial source-selection leases, whole-tree content freeze, atomic uncooperative-writer exclusion and transaction semantics remain separate.

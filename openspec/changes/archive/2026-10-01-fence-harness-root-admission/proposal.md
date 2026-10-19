## Why

Harness target roots and output-directory ancestors can be resolved through links before their authorized repository or machine scope is enforced. A root must not become its own containment authority merely because it resolves successfully.

## What Changes

- Admit harness target roots against their authorized scope before output work or canonicalization discards the logical root.
- Preserve scope aliases and contained directory-root aliases; reject escaping, dangling, cyclic and non-directory roots.
- Require real directory ancestors below an admitted root, with descriptor-relative creation of missing output directories.
- Preserve Codex partition preflight, explicit absolute CODEX_HOME relocation, independent target progress and read-only checking.

## Capabilities

### Modified Capabilities

- `materialization`: scope-contained root admission and output-directory ancestors.

## Impact

Harness destination resolution and materialization directory preparation on macOS/Linux. No config/lock formats, global processing ceilings or conversion-destination policy changes. Publication leaf binding, source/support reads, aggregate payload reads, portable manifests, restore and durable ownership remain separate work.

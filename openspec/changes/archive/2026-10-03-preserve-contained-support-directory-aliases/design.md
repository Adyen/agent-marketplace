## Context

Real skill support copying and the read-only portable digest both classify directory aliases as files. File snapshots correctly refuse directory-shaped leaves, making installation and checking incompatible with contained directory aliases.

## Goals / Non-Goals

- Keep directory resolution anchored to the captured source scope and traverse the resolved directory within it.
- Reuse existing file snapshots and captured-directory resolution.
- Do not change destination alias policy, add size/work thresholds, or support Windows.

## Decisions

Use shared support-entry classification and strict captured-directory resolution. Separate the logical output path from the resolved source path. Detect repeated active directory paths to refuse traversal cycles. Keep captured regular singly-linked file reads unchanged.

## Risks / Trade-offs

Directory aliases expand content at their logical output names. Active-path cycle detection allows independent aliases to the same directory while refusing recursive expansion. Existing symlink-mode cache refresh behavior remains unchanged; file-mode unchanged native output and all read-only checks remain no-ops.

## Migration Plan

No configuration or ownership-state format changes.

## Open Questions

None.

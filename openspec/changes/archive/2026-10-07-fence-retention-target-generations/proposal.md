## Why

Rotate enumerates target directories beneath a captured backup root but opens each target without retaining its admitted object identity. Replacing an enumerated target with a real foreign directory imports its complete backup names and lets retention delete foreign members while reporting success.

## What Changes

- Retain target no-follow directory metadata/identity before descent; match captured target descriptor to original identity before ordinary/Codex discovery.
- Share admitted parent descriptor across existing ordinary/Codex retention consumers, close per-target descriptors within each iteration and revalidate named parent before return.
- Preserve member generation/strict reader/deletion checks, retention grouping/order/defaults, supported initial backup-root aliases, missing-root no-op and causal errors.

## Capabilities

### Modified Capabilities

- `backup-recovery`: retention requires the original admitted target-directory generation before importing backup members.

## Impact

R2 child of #577, stack #560. Existing source-metadata/identity/captured-directory/parent-verification primitives reused; no new helper/type/public API/dependency/budget/source-comment/content-policy/durability change. Original sampled directory selection does not establish an atomic uncooperative-writer exclusion, complete namespace snapshot or transaction across retained/removed members. Namespace preparation and final admitted-case/report-appendix audit remain open.

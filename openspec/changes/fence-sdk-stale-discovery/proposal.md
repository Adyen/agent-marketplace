## Why

SDK filepath.WalkDir reopens directory paths after classification. Replacing an admitted real directory supplies foreign stale members that public sync deletes; dry-run also imports their claims.

## What Changes

- Reuse captured directory/native-root operations for SDK stale traversal instead of path-based WalkDir.
- Match original output root and each opened directory to admitted metadata, validate original ancestry before/after reading members and return generation conflict on replacement.
- Retain original parent identities and no-follow leaf metadata in candidates, preserve sorted paths, special-file refusal, transaction exclusions and absence compatibility.

## Capabilities

### Modified Capabilities

- `materialization`: SDK stale directory discovery authority.

## Impact

R2 child of #549 in stack #478. Existing private function/shared primitives; no new helper/type/public API/dependency/budget/source-comment change. Stale deletion/reporting/empty-parent cleanup remain next; no whole transaction or atomic uncooperative-writer exclusion.

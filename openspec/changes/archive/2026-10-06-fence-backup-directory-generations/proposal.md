## Why

Backup selects a source directory's metadata and later walks the name under its parent without retaining that admitted directory generation. A replacement directory can supply foreign members to a successful archive, confirmed through public backup creation and recovery.

## What Changes

- Pass the classified directory metadata into the existing archive walk and match a captured strict directory descriptor to that original object before traversal.
- Walk/read metadata and regular payloads below the selected captured directory, preserving original archive path prefixes, root-dot omission and dot-prefixed managed paths.
- Revalidate the original named directory generation after traversal, propagating capture/close refusal and allowing creator archive cleanup to remove only own output.

## Capabilities

### Modified Capabilities

- `backup-recovery`: a selected archive directory retains its admitted generation through member import.

## Impact

R2 child of #570, stack #560. Existing archiveDir, metadata/payload readers and captured directory primitives reused; no new helper/type/public API/dependency/budget/source-comment change. Original top-level selection, deeper directory generation transitions and complete-tree snapshots remain distinct audit cases, not claimed fixed by this slice. No whole archive transaction or uncooperative-writer exclusion is introduced.

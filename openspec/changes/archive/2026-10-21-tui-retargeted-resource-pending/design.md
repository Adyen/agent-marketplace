## Context

The materialization drift report lists Missing, Broken and Orphaned items per output. A resource retargeted from Factory Droid to Claude Code has a Missing item (Claude Code) and an Orphaned item (Factory Droid) under the same resource key. `buildRows` and `applyDriftItems` each built a name-keyed map by inserting Missing, then Broken, then Orphaned, so the last insert won.

## Goals / Non-Goals

**Goals:** a selected resource shows the drift that describes its own pending work.

**Non-Goals:** showing per-harness drift on one row; the orphaned copy is still removed by sync and still counts in the drift total.

## Decisions

- **Precedence broken > missing > orphaned.** Broken already overrode Missing; only the orphaned copy moves to the lowest rank, because it is cleanup of a former target rather than state of the selection.
- **One helper for both builders.** `driftItemsByResource` replaces the two duplicated maps, so the inventory and final stages cannot disagree.
- **An orphaned copy on a row is pending sync.** `applyDriftItems` skips available rows and `buildRows` attaches drift only to resolved (selected) resources, so a row never represents a deselected resource with an orphaned copy. Every orphaned item on a row is a stale copy in a former target, for example after removing a harness, and sync will remove it, so the row shows `◐ enabled (pending sync)` in Pending sync. The `◌ orphaned` status and its glyph style were reachable only through this misleading path and are removed.
- **Rows keep one identity.** `rowKey` gave a row with orphaned drift a separate `orphan\x00<name>` key, so the selection fell back to its old index once sync cleared the stale copy. The branch is removed; such a row is an ordinary selected row and keys as one.

## Risks / Trade-offs

- [A row hides that an old copy is pending removal] → the footer drift count still includes it, and sync removes it.

## Context

`appendUnavailableSelectionRows` turns resolver `SelectedNotFoundError`s and catalog-load failures into unavailable rows. `readSnapshotStaged` treats any unavailable row as a reason to report `driftFailed`. The resolver already classifies non-required, non-collection misses as soft (`resolver.IsSoftSelectedNotFound`), and the pipeline reports them as warnings, so the TUI disagrees with the CLI about the same configuration.

## Decisions

- Add `UnavailableSoft` to `row`. It is set only for candidates built from resolver errors where `IsSoftSelectedNotFound` is true. Catalog-unavailable candidates and required or collection misses stay non-soft.
- `rowsHaveUnavailable` counts only non-soft unavailable rows. Its callers (snapshot drift state, footer drift segment, failures-or-drift) therefore ignore soft rows without further changes.
- Every other use of `UnavailableReason` (row status, detail pane, toggle eligibility, bulk skip reasons) is unchanged, so the missing skill still renders as an unavailable row with its reason.
- The soft not-found error still reaches the snapshot through the inspection issues as a warning diagnostic.

## Alternatives Considered

- Dropping soft rows from the inventory: rejected because users need to see the missing selection and why it is unavailable.
- Downgrading the drift state only when the missing skill is the sole unavailable row: rejected because it would couple drift policy to row count rather than to selection severity.

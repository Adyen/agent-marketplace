## Why

When quitting the TUI with pending drift, the confirmation modal only displays a bare count (`"N pending changes not synced. Sync+quit?"`) without telling the user which resources will be written or removed. Worse, `DriftReport.PendingCount()` includes `Broken` error items alongside syncable `Missing` and `Orphaned` items, and `sync+quit` exits without checking whether sync actually cleared the drift—so users get trapped in a loop where confirming "yes" and restarting still prompts to sync the same unresolved items.

## What Changes

- Display the concrete pending items inside the quit-sync confirmation modal (`+ install <resource>` for missing items, `- remove <resource>` for orphaned items), capped to fit bounded terminal sizes with a `+ N more` summary line.
- Separate syncable pending drift (`Missing` + `Orphaned`) from unfixable `Broken` diagnostics: when only `Broken` items exist and zero syncable changes remain, pressing `q` does not prompt to "Sync+quit?" (which cannot resolve broken items).
- When `sync+quit` completes without a hard sync error, run the authoritative post-sync check before exiting; if syncable drift still remains unresolved, keep the TUI open and surface the diagnostics/outcome rather than silently quitting into a repeated sync loop. Preserve existing error-return identity when sync itself returns a hard error.

## Capabilities

### Modified Capabilities

- `tui-feedback`: list concrete pending changes in the quit-sync modal and distinguish syncable drift from broken diagnostics.

## Impact

`internal/materialize/drift_compat.go`, `internal/tui/app.go`, `internal/tui/help.go`, `internal/tui/forms.go`, unit tests, and `tools/tui-qa/tests/`.

## Release note

The quit-sync confirmation modal now lists the exact resources that will be installed or removed, stops prompting to sync when only unfixable errors exist, and keeps the TUI open with diagnostics if drift remains after sync.

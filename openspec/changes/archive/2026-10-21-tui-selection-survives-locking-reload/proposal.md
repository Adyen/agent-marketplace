## Why

In a workspace whose Git source has no acquisition lock yet, the first selection edit creates the lock, so the post-write reload loads that source from its locked snapshot instead of the mutable cache. Every Git row's catalog key changes from the cache key to the snapshot identity. Rebuilds restore the selection by row key, which includes the catalog key, so the restore failed and the cursor kept its old index: after enabling `alpha-skill` the selection landed on `beta-skill`. An open detail view closed for the same reason. The same applies whenever an update moves the locked commit. The `org-marketplace-lock-failure` journey only passed when it saw the screen before that reload, which made it fail intermittently (#830).

## What Changes

- Rebuild anchors also record a catalog-independent resource identity (kind, name, normalized source URL, ref and path). When the row key no longer matches, the list selection and an open detail view fall back to the single row with that identity. Ambiguous matches are never guessed.
- `tui-navigation` specifies that reload selection follows the resource.
- The `org-marketplace-lock-failure` journey asserts the settled state after its local toggle (`Pending sync (2)`, `~ 3 out of sync`, `alpha-skill` selected) instead of the transient pre-reload screen.

## Capabilities

### New Capabilities

### Modified Capabilities
- `tui-navigation`: reload selection follows the resource.

## Impact

`internal/tui` rebuild anchoring (`flatitems.go`) and open-detail rebuild (`app.go`). Row keys used for detail requests, forms and ordering are unchanged.

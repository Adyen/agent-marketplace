## Why

`agent-marketplace list` omits Marketplace selections entirely and reports every installed Marketplace resource as out of sync right after a successful sync. Collections are virtual: the resolver expands a selected collection into its members and drops the collection artifact, so an installed collection is reported as available in `list` and in the TUI while its members show installed.

- `list` resolves Marketplace selections without `MarketplaceArtifactsFromCache`, which only `status` and `doctor` set (commit 06f722faf).
- `resolver.Resolve` removes selected collection roots during expansion, and the Marketplace resolver skips collection pins whose leaves are expanded, so `ResolveCatalogView` emits the root as an unselected catalog winner.

## What Changes

- `list` resolves Marketplace selections from the verified local cache, matching `sync` and `status`.
- Selected collection roots (Git, local, and direct Marketplace pins) are reported as display-only selections, so `list` and the TUI show them once as installed.
- The TUI provisional stage renders selected collections as installed-pending rows instead of available rows.
- No change to materialization, drift, sync, locks, or configuration writes.

## Capabilities

### New Capabilities

- `resource-listing`: list behavior for Marketplace selections and installed collections.

### Modified Capabilities

- `tui-startup`: installed collection rows in the provisional and verified stages.

## Context

Collections are expanded during `resolver.Resolve` (phase 4). The collection artifact is removed from the resolved list, so any consumer that reads only `Resolved` cannot see that the collection is selected. Marketplace collection pins are not catalog entries, so `ResolveCatalogView` cannot recover them either.

## Decisions

- **Selected roots are reported, not materialized.** `resolver.WithSelectedCollections` captures the collection artifacts that `expandCollections` is about to expand. The sink is replaced on each selection pass, because `selectArtifacts` can run twice when a required member displaces a repository selection; only the final selection remains.
- **Marketplace roots come from configuration.** `pipeline.Resolve` adds direct, non-excluded collection pins through `marketplace.PinArtifact`, the shared identity builder also used by the TUI. The root's leaves still come from the Marketplace resolver.
- **Display selection is separate from materialization.** `ResolveResult.DisplayArtifacts()` returns `Resolved` followed by `SelectedCollections`. `list` and the TUI verified inventory use it. Materialize, dry-run drift, and sync keep reading `Resolved`.
- **`list` uses verified cache evidence.** `MarketplaceArtifactsFromCache` is set so Marketplace selections resolve to artifacts from the verified cache, with no download and no config or lock write.

## Risks

- Display-only collection roots exist only in presentation. Any new consumer that needs materializable state must keep using `Resolved`.

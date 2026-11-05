## Context

`planMarketplaceUpdates` and `planMarketplaceCollectionUpdate` in `internal/pipeline/update.go` call `Resolver.LatestRevision` for each user-managed pin and return on the first error. `LatestRevision` in `internal/marketplace/install.go` wraps the client's `*APIError` from `GetLatestResourceRevision` as `resolve latest resource revision: ...`, and also returns `*APIError` 404 for an unknown registry from `GetRegistryBySlug`, so a bare status check cannot tell a missing resource from a missing registry.

## Goals / Non-Goals

**Goals:** a resource-level 404 is skipped and reported while every other pin proceeds; the command reports it and exits 1.

**Non-Goals:** rename detection, automatic replacement, any change to non-404 failures, Git update semantics, the TUI, or file formats.

## Decisions

- `LatestRevision` returns a typed missing-resource error (wrapping the original `*APIError`) only when `GetLatestResourceRevision` answers 404. Registry lookups and every later check keep their current errors, so a registry 404 stays fatal.
- Planning records a missing pin as a skipped plan with a distinct reason and continues. Skipped plans are never staged, so the missing pin's lock entry and, for a collection, its members stay byte-identical.
- `pipeline.Update` keeps its existing flow (nothing actionable means no staging or sync) and, when any pin was missing, returns the result together with a typed error listing every missing pin. `runUpdate` writes the normal report from that result first, then returns the error so Cobra prints it and the process exits 1.
- The hint names the pin's installed name when set, otherwise its name, in `agent-marketplace remove <name>`, and tells the user to install the new name.

## Risks / Trade-offs

- A run that exits 1 has still committed other updates. The report printed before the error lists them, and `sync --check` stays clean because only applied pins changed.
- Sibling Bead y0n5.1 may also edit `internal/pipeline/update.go`; conflicts are resolved at rebase time.

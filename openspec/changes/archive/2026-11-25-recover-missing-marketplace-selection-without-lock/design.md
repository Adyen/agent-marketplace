## Context

Local reproduction (2026-11-18): a repository configuration selecting `team/skill/going-away` and `team/skill/keep-me` from a Marketplace, with no `.agents/lock.yaml`, after `going-away` was removed upstream. `sync` failed with `create Marketplace lock ...: resolve team/skill/going-away: resolve resource revision: marketplace error 404: "resource not found"` and installed nothing; `sync --prune-stale` printed `prune-stale skipped: the catalog could not be trusted`; `remove going-away --scope repo` and `status` failed with `v4.marketplace-lock-required`.

## Goals / Non-Goals

**Goals:** give the user a working `remove`, and let the rest of the layer lock and install.

**Non-Goals:** keeping a resource that was never locked (there is no revision to pin); changing read-only modes, which make no Marketplace requests and keep reporting `v4.marketplace-lock-mismatch`; changing organization lock behavior; changing `update` (separate change `keep-removed-marketplace-pins`).

## Decisions

- **Removal is a configuration edit.** Removing a selection needs no acquisition evidence for that selection, so the lock-required refusal does not apply to it. Unsupported or invalid configuration roots still refuse.
- **Missing optional selections warn, required ones fail.** This matches Git selection misses in `resource-selection` and the TUI's optional-miss rule in `tui-startup`.
- **Only a 404 on the latest-revision lookup counts as removed.** Other Marketplace failures (network, 5xx, registry-level 404, instance mismatch) still fail the run without writing a lock.

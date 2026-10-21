## Why

Plugin support (issue #6, v5.0.0) delivers plugins only as decomposed
members; a `delivery: native` request still gets the
`plugin.delivery.pending` warning. Native delivery to Claude Code and Codex
needs registration that preserves handwritten settings and owns only what it
wrote, a per-request delivery choice that follows the harness catalog, and
safe transitions between native and decomposed delivery. Each harness turns
native delivery on later, when its isolated probe passes.

## What Changes

- A plugin request's `delivery` (`auto`, `native`, `decompose`) is resolved
  per harness target from its harness catalog: `auto` chooses native exactly
  where the catalog declares native delivery verified, and sync reports the
  choice per target. An explicit `native` request to an unverified target
  fails with `plugin.delivery.native-unverified` and is never decomposed.
- Native delivery publishes the package generation into a native plugin
  catalog owned by Agent Marketplace under the state root and registers it
  only through owned entries: Claude Code `settings.json`
  (`extraKnownMarketplaces`, `enabledPlugins`) and Codex `config.toml`
  (`marketplaces`, `plugins`). Entries sharing a file with hooks or MCP
  servers are written in one combined mutation. No trust state, vendor
  policy, or private plugin cache is written.
- Native delivery to a target without registration members fails closed.
- Harness catalogs can declare that a native refresh requires a restart;
  sync and status then report `reload-required`.
- Switching a plugin instance between native and decomposed delivery stages
  the new delivery before retiring the old one, in one recoverable sequence.
- Every shipped harness catalog keeps `delivery.native.status: unverified`,
  so shipped behaviour for `auto` stays decomposed. Tests force native
  delivery with a test-only verified catalog.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `plugin-delivery`: delivery is chosen per plugin request, native
  registration uses owned entries only, and delivery transitions never
  activate a plugin twice.

## Impact

- `pkg/harness-compilation`: registration members in `NativeLayout`, the
  native refresh field in harness catalogs, and new diagnostic codes.
- `internal/pipeline`, `internal/materialize`, `internal/ownership`,
  `internal/dependencytxn`: delivery resolution, the native plugin catalog,
  owned registration entries, and delivery transitions.
- `cmd/agent-marketplace`: sync and status reporting of the chosen delivery
  and `reload-required`.
- `e2e`: crash-recovery coverage for an interrupted delivery switch.
- Shipped behaviour without a verified catalog is unchanged. Native probes,
  Factory Droid registration, git-sourced native catalog entries, and
  documentation remain with later changes.

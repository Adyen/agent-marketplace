## Why

Plugin support (issue #6, v5.0.0) can register plugins natively, but Factory
Droid has no native registration yet, and its shipped catalog keeps native
delivery `unverified`. Factory documents a registration path that its own
settings trigger: a marketplace declared in `extraKnownMarketplaces` with a
`local` source, and a plugin listed in `enabledPlugins`, is installed by Droid
during startup at the scope of the settings file that declares it. Agent
Marketplace can use that path through owned settings entries without writing
Droid's plugin cache or installed-plugin records.

## What Changes

- An Agent Marketplace-owned Factory catalog is published as a marketplace
  Droid accepts: `.factory-plugin/marketplace.json` names the catalog and an
  owner and lists each plugin with a source relative to the catalog root.
- Factory registration is written to `.factory/settings.local.json` at
  repository scope and to `~/.factory/settings.json` at user scope, as owned
  `extraKnownMarketplaces` (with a `local` source) and `enabledPlugins`
  entries. Handwritten settings are preserved.
- An isolated native probe for Factory Droid joins the harness validation
  workflow. Factory's catalog declares native delivery `verified` only when
  that probe passes; otherwise it stays `unverified`, `auto` keeps
  decomposing for Factory, and explicit `delivery: native` stays refused.
- The released-client gate witnesses that the pinned released reader still
  passes on committed state after native Factory writes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `plugin-delivery`: native Factory Droid registration files per scope and
  the Factory catalog marketplace shape.

## Impact

- `pkg/harness-compilation`: the Factory registration member and, when its
  probe passes, the Factory plugin catalog status.
- `internal/materialize`: the Factory native catalog layout and registration
  entries.
- `e2e` and `tools/harness-compat`: the Factory native probe and the
  released-client witness.
- Claude Code and Codex native delivery, git-sourced native catalog entries,
  user documentation, and the delivery-resolution rules are unchanged.

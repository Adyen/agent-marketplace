## Why

Plugin support (issue #6, v5.0.0) can register plugins natively, but Factory
Droid's shipped catalog keeps native delivery `unverified`. Agent Marketplace
has added the Factory-owned catalog and settings registration as groundwork.
In CI runs 40682001059 and 40698987206, Droid 0.251.0 recorded the owned
marketplace but did not install the enabled plugin in headless metadata
sessions. Native-delivery verification moves to a follow-up.

## What Changes

- An Agent Marketplace-owned Factory catalog is published as a marketplace
  Droid accepts: `.factory-plugin/marketplace.json` names the catalog and an
  owner and lists each plugin with a source relative to the catalog root.
- Factory registration is written to `.factory/settings.local.json` at
  repository scope and to `~/.factory/settings.json` at user scope, as owned
  `extraKnownMarketplaces` (with a `local` source) and `enabledPlugins`
  entries. Handwritten settings are preserved.
- Factory native delivery remains `unverified`; `auto` continues to decompose
  Factory plugins and explicit `delivery: native` stays refused.
- The released-client gate witnesses that the pinned released reader still
  passes on committed state after native Factory writes through an explicitly
  verified test catalog.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `plugin-delivery`: native Factory Droid registration files per scope and
  the Factory catalog marketplace shape.

## Impact

- `pkg/harness-compilation`: the Factory registration member and catalog.
- `internal/materialize`: the Factory native catalog layout and registration
  entries.
- `e2e`: Factory registration coverage and the released-client witness.
- Factory verification beyond the registration groundwork, Claude Code and
  Codex native delivery, git-sourced native catalog entries, and the
  delivery-resolution rules are unchanged.

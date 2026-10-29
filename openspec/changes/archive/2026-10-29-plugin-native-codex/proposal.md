## Why

Plugin support (issue #6, v5.0.0) can register plugins natively, but the
Codex registration groundwork does not match what Codex reads. Codex CLI
0.146.1 rejects a `[marketplaces.<name>]` entry written as `source = "local"`
with a `path`, and silently drops catalog entries with absolute `directory`
sources. Even with the documented shape, a registered local marketplace and
an enabled plugin are not installed headlessly; only Codex's own
`codex plugin add` installs them. Codex native delivery therefore stays
`unverified`, and verification moves to a follow-up.

## What Changes

- Codex registration declares the Agent Marketplace catalog as a
  `[marketplaces.<catalog>]` entry with `source_type = "local"` and `source`
  set to the absolute catalog root, and enables each plugin through
  `[plugins."<plugin>@<catalog>"]` with `enabled = true`. Owned entries from
  the earlier shape are replaced in one sync. Handwritten configuration is
  preserved and no trust entry is written.
- The Codex catalog's `.agents/plugins/marketplace.json` lists each plugin
  with a `local` source relative to the catalog root, an installation and
  authentication policy, and a category, over linked generations.
- The Codex harness-compatibility job runs a shape probe: after sync, Codex
  lists the plugin from the registered marketplace, with a negative control
  before sync. The probe never installs the plugin, writes trust, or calls a
  model.
- Codex native delivery remains `unverified`; `auto` continues to decompose
  Codex plugins and explicit `delivery: native` stays refused.
- The released-client gate witnesses that the pinned released reader still
  passes on committed state after native Codex writes through an explicitly
  verified test catalog.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `plugin-delivery`: native Codex registration entries per scope and the
  Codex catalog marketplace shape.

## Impact

- `internal/materialize`: the Codex native catalog manifest and registration
  entries.
- `e2e`: Codex registration coverage, the Codex shape probe, and the
  released-client witness.
- `docs/harness-validation/codex.md`: the observed Codex plugin behavior.
- Claude Code and Factory Droid delivery, repository-scope placement of the
  Codex registration, git-sourced native catalog entries, and the
  delivery-resolution rules are unchanged.

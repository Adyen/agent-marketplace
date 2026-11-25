## ADDED Requirements

### Requirement: Factory Droid repository-scope hooks use settings.local.json and migrate retired hooks.json

At repository scope, Factory Droid hooks SHALL materialize into `.factory/settings.local.json` under the `hooks` member without `_managed_by` markers, leaving `.factory/settings.json` unchanged and preserving existing `enabledPlugins`, `extraKnownMarketplaces`, handwritten hooks, and unrelated settings in `.factory/settings.local.json`.

When `.factory/hooks.json` contains entries proven owned by ownership state, `sync` SHALL atomically write the desired repository-scope hooks into `.factory/settings.local.json` under `hooks`, prune only the ownership-proven entries from `.factory/hooks.json`, remove any event key whose array becomes empty, delete `.factory/hooks.json` when empty, clear the pruned ownership records, and keep handwritten entries intact. Marked legacy entries in `.factory/hooks/hooks.json` SHALL continue migrating out of its top-level event map into `.factory/settings.local.json#hooks`.

#### Scenario: Factory Droid repository-scope hooks materialize into settings.local.json

- **WHEN** a repository-scope hook is synced to Factory Droid while `.factory/settings.json` declares a hook for the same event and `.factory/settings.local.json` carries plugin registrations
- **THEN** the managed hook MUST be written under `hooks` in `.factory/settings.local.json` without `_managed_by` markers
- **AND** `.factory/settings.json` MUST remain byte-identical, `.factory/hooks.json` MUST NOT be created, and existing `enabledPlugins` and `extraKnownMarketplaces` members in `.factory/settings.local.json` MUST remain intact

#### Scenario: Owned repository-scope Factory hooks migrate out of hooks.json

- **WHEN** `.factory/hooks.json` holds ownership-proven Factory Droid hooks and `sync` runs
- **THEN** the desired hooks MUST appear under `hooks` in `.factory/settings.local.json`
- **AND** only ownership-proven entries MUST be pruned from `.factory/hooks.json`, empty event keys MUST be removed, and `.factory/hooks.json` MUST be deleted when empty

## ADDED Requirements

### Requirement: Factory Droid machine-scope hooks use settings.local.json and migrate retired hooks.json

At machine scope, Factory Droid hooks SHALL materialize into `~/.factory/settings.local.json` under the `hooks` member, using matcher-group entries without `_managed_by` markers, and SHALL leave `~/.factory/settings.json` unchanged.

When `~/.factory/hooks.json` contains entries proven owned by ownership state, `sync` SHALL atomically write the desired machine-scope hooks into `~/.factory/settings.local.json` under `hooks`, prune only the ownership-proven entries from `~/.factory/hooks.json`, remove any event key whose array becomes empty, delete `~/.factory/hooks.json` when no entries remain, clear the pruned ownership records, and record a safety backup first. Handwritten entries in `~/.factory/hooks.json` SHALL retain their bytes and order. Deselecting all machine-scope Factory Droid hooks SHALL still prune owned entries from `~/.factory/hooks.json`. Malformed JSON, duplicate JSON keys, unsafe paths, or corrupt ownership state for `~/.factory/hooks.json` SHALL fail closed for Factory Droid machine scope without modifying `~/.factory/settings.local.json` or `~/.factory/hooks.json`. Marked legacy entries in `~/.factory/hooks/hooks.json` SHALL continue migrating out of its top-level event map into `~/.factory/settings.local.json#hooks`.

#### Scenario: Factory Droid machine-scope hooks materialize into settings.local.json

- **WHEN** a machine-scope hook is synced to Factory Droid while `~/.factory/settings.json` declares a hook for the same event
- **THEN** the managed hook MUST be written under `hooks` in `~/.factory/settings.local.json` without `_managed_by` markers
- **AND** `~/.factory/settings.json` MUST remain byte-identical and `~/.factory/hooks.json` MUST NOT be created

#### Scenario: Owned machine-scope Factory hooks migrate out of hooks.json

- **WHEN** `~/.factory/hooks.json` holds only ownership-proven Factory Droid hooks and `sync` runs
- **THEN** the desired hooks MUST appear under `hooks` in `~/.factory/settings.local.json`
- **AND** `~/.factory/hooks.json` MUST be deleted and a safety backup MUST preserve its prior bytes

#### Scenario: Handwritten entries in retired Factory hooks.json stay intact without empty event arrays

- **WHEN** `~/.factory/hooks.json` holds both an ownership-proven hook and a handwritten hook, and `sync` runs
- **THEN** only the ownership-proven entry MUST be pruned from `~/.factory/hooks.json`
- **AND** the handwritten entry MUST keep its bytes and order, and any event key left empty by pruning MUST be removed from `~/.factory/hooks.json`

#### Scenario: Deselecting all machine-scope Factory hooks still prunes retired hooks.json

- **WHEN** `~/.factory/hooks.json` holds an ownership-proven hook and every machine-scope Factory Droid hook is deselected before `sync` runs
- **THEN** the ownership-proven entry MUST be pruned and `~/.factory/hooks.json` MUST be deleted when empty

#### Scenario: Malformed retired Factory hooks.json fails closed

- **WHEN** `~/.factory/hooks.json` is malformed or has corrupt ownership state and `sync` runs for Factory Droid machine scope
- **THEN** Factory Droid machine scope MUST fail with a coded error
- **AND** neither `~/.factory/settings.local.json` nor `~/.factory/hooks.json` MUST be modified

#### Scenario: Legacy marked Factory machine hooks migrate to settings.local.json

- **WHEN** `~/.factory/hooks/hooks.json` contains a marked top-level event hook entry and `sync` runs
- **THEN** the managed hook MUST be written under `hooks` in `~/.factory/settings.local.json`
- **AND** the marked entry MUST be stripped from `~/.factory/hooks/hooks.json` while any handwritten sibling stays intact

### Requirement: Factory Droid repository sync tolerates hooks already satisfied in settings.local.json

At repository scope, before writing a selected Factory Droid hook into `.factory/hooks.json`, Agent Marketplace SHALL recognize the same selected hook when it is already completely satisfied in `.factory/settings.local.json` under `hooks`, proven either by clean durable ownership or satisfaction proof matching the exact resource and version-1 source fingerprint, or by complete source-derived independent satisfaction (all expected canonical entries present in `.factory/settings.local.json#hooks` together with exact verified regular support files under `.factory/hook-support/<source>/<hook>/`).

When satisfied, `sync`, `sync --locked`, and `sync --check` SHALL succeed without writing `.factory/hooks.json`, modifying `.factory/settings.local.json`, or creating ownership state. Incomplete, mismatched, or unproven entries SHALL retain ordinary `.factory/hooks.json` output without adopting or modifying `.factory/settings.local.json`. Malformed, ambiguous, or unsafe `.factory/settings.local.json` SHALL fail closed with `hook.native-tolerance-refused` (or `json.duplicate-key`) without mutating repository hook files or ownership state.

#### Scenario: Complete Factory settings.local.json hooks satisfy repository sync without writing hooks.json

- **WHEN** `.factory/settings.local.json` under `hooks` and `.factory/hook-support/` completely match a selected repository Factory Droid hook from a verified source and an independent client runs `sync`, `sync --locked`, and `sync --check`
- **THEN** all three commands MUST succeed without creating `.factory/hooks.json` or changing `.factory/settings.local.json` or ownership state

#### Scenario: Mismatched Factory settings.local.json hooks do not suppress repository hooks.json

- **WHEN** `.factory/settings.local.json` under `hooks` contains a hook whose matcher, command, or support file differs from the selected repository Factory Droid hook
- **THEN** `sync` MUST retain ordinary `.factory/hooks.json` output and MUST NOT adopt or modify `.factory/settings.local.json`

#### Scenario: Malformed Factory settings.local.json fails closed during repository forward check

- **WHEN** `.factory/settings.local.json` contains malformed JSON or duplicate keys and `sync` or `sync --check` runs for a selected repository Factory Droid hook
- **THEN** Factory Droid repository scope MUST fail closed with `hook.native-tolerance-refused` or `json.duplicate-key`
- **AND** `.factory/settings.local.json` and `.factory/hooks.json` MUST remain unchanged

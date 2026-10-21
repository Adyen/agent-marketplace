## ADDED Requirements

### Requirement: Delivery is chosen per plugin request

A plugin request's `delivery` SHALL be `auto`, `native`, or `decompose`,
defaulting to `auto`, and SHALL apply to every harness target the request is
delivered to. `auto` SHALL choose native delivery for a harness target
exactly when its shipped catalog declares native delivery verified, and
decomposed delivery otherwise. Sync SHALL report the delivery chosen per
harness target.

#### Scenario: Auto decomposes where native delivery is unverified

- **WHEN** a plugin with `delivery: auto` is synced to Cursor and to a harness
  target whose catalog declares native delivery verified
- **THEN** Cursor MUST receive decomposed members, the other harness target
  MUST receive a native registration, and sync output MUST name both choices

#### Scenario: Native delivery never falls back to decomposed output

- **WHEN** a plugin with `delivery: native` is synced to a harness target
  whose catalog does not declare native delivery verified
- **THEN** that instance MUST fail with `plugin.delivery.native-unverified`
  and no decomposed members MUST be written for it

### Requirement: Native registration uses owned entries only

Native delivery SHALL publish the package generation into a native plugin
catalog owned by Agent Marketplace and SHALL register it only through owned
entries in the harness target's native settings. Handwritten entries, keys,
order, and comments SHALL be preserved. Entries sharing a file with hooks or
MCP servers SHALL be written in one combined mutation. Agent Marketplace
SHALL NOT write trust state, vendor-managed policy, or private runtime
plugin caches.

#### Scenario: Claude registration preserves handwritten settings

- **WHEN** a plugin is delivered natively to Claude Code at repository scope
  and `.claude/settings.json` has handwritten `enabledPlugins` entries and
  owned hooks
- **THEN** the file MUST gain the owned registration entries, keep every
  handwritten entry and comment, and keep the owned hooks

#### Scenario: Registration without ownership state is not pruned

- **WHEN** a registration entry matching Agent Marketplace's naming exists
  but ownership state does not record it
- **THEN** sync MUST leave it unchanged and report the collision

#### Scenario: Codex registration writes no trust

- **WHEN** a plugin is delivered natively to Codex
- **THEN** `config.toml` MUST gain only the owned plugin entries and no
  project trust entry

#### Scenario: Reload requirement is reported

- **WHEN** a native generation changes for a harness target whose catalog
  declares that refresh requires a restart
- **THEN** sync and status MUST report `reload-required` for that instance

### Requirement: Delivery transitions never activate a plugin twice

Changing a plugin instance between native and decomposed delivery SHALL
stage the new delivery first. It SHALL then retire only proven owned
outputs and registrations of the old delivery, in one recoverable sequence.

#### Scenario: Switching to decompose removes the native registration

- **WHEN** `delivery` changes from `native` to `decompose` and sync completes
- **THEN** the decomposed members MUST be installed and the owned native
  registration entries MUST be gone

#### Scenario: Interrupted delivery switch recovers without duplicates

- **WHEN** sync is interrupted after staging the new delivery and before
  retiring the old one
- **THEN** the next recovery-capable command MUST complete the switch, and no
  state with both deliveries active MUST remain


## ADDED Requirements

### Requirement: Claude Code native delivery is verified at user scope by a Claude CLI probe

The Claude Code plugin catalog SHALL declare native delivery verified at user
scope with the `claude-native-plugin` probe and unverified at repository
scope. The probe SHALL run a real Claude Code CLI offline with an isolated
home and no model call. At user scope, before sync it SHALL confirm that
Claude does not list the plugin. After sync it SHALL confirm that Agent
Marketplace wrote none of Claude's installed-plugin records,
known-marketplace records, plugin cache, or trust state. It SHALL then
establish workspace trust as the user would, let Claude register the
declared marketplace and install the plugin itself, and require Claude's own
installed-plugin record, `claude plugin list` reporting the plugin enabled,
and Claude initialization listing the plugin's namespaced skill. At
repository scope the probe SHALL confirm that a plugin with `delivery: auto`
receives decomposed members and no native Claude registration. Agent
Marketplace SHALL NOT write Claude trust state.

#### Scenario: Claude probe loads the registered plugin at user scope

- **WHEN** a plugin is delivered natively to Claude Code at user scope and the
  probe establishes workspace trust and starts a Claude session
- **THEN** Claude MUST record the plugin as installed, `claude plugin list`
  MUST report it enabled, and Claude initialization MUST list its namespaced
  skill, while the pre-sync negative control MUST have listed neither

#### Scenario: Claude probe fails when sync wrote Claude-owned plugin state

- **WHEN** Claude's installed-plugin records, known-marketplace records,
  plugin cache, or trust state exist after sync and before the probe starts a
  Claude session
- **THEN** the probe MUST fail and name the path that exists

#### Scenario: Claude probe confirms repository scope stays decomposed

- **WHEN** a plugin with `delivery: auto` is synced to Claude Code at
  repository scope
- **THEN** the probe MUST find the plugin's decomposed members and no native
  Claude registration in the repository's Claude settings

#### Scenario: Auto delivers Claude plugins natively at user scope only

- **WHEN** a plugin with `delivery: auto` is synced to Claude Code at user
  scope and, separately, at repository scope
- **THEN** the user-scope sync MUST write a native registration and no
  decomposed members, the repository-scope sync MUST write decomposed members
  and no native registration, and sync output MUST name each choice

#### Scenario: Explicit native delivery is refused at repository scope for Claude

- **WHEN** a plugin with `delivery: native` is synced to Claude Code at
  repository scope
- **THEN** that instance MUST fail with `plugin.delivery.native-unverified`
  and no decomposed members MUST be written for it

## MODIFIED Requirements

### Requirement: Delivery is chosen per plugin request

A plugin request's `delivery` SHALL be `auto`, `native`, or `decompose`,
defaulting to `auto`, and SHALL apply to every harness target the request is
delivered to. `auto` SHALL choose native delivery for a harness target
exactly when its shipped catalog declares native delivery verified for the
scope the request is delivered at, and decomposed delivery otherwise. Sync
SHALL report the delivery chosen per harness target.

#### Scenario: Auto decomposes where native delivery is unverified

- **WHEN** a plugin with `delivery: auto` is synced to Cursor and to a harness
  target whose catalog declares native delivery verified
- **THEN** Cursor MUST receive decomposed members, the other harness target
  MUST receive a native registration, and sync output MUST name both choices

#### Scenario: Native delivery never falls back to decomposed output

- **WHEN** a plugin with `delivery: native` is synced to a harness target
  whose catalog does not declare native delivery verified for the request's
  scope
- **THEN** that instance MUST fail with `plugin.delivery.native-unverified`
  and no decomposed members MUST be written for it

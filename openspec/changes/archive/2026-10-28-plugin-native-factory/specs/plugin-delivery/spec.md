## MODIFIED Requirements

### Requirement: Native registration uses owned entries only

Native delivery SHALL publish the package generation into a native plugin
catalog owned by Agent Marketplace and SHALL register it only through owned
entries in the harness target's native settings. Handwritten entries, keys,
order, and comments SHALL be preserved. Entries sharing a file with hooks or
MCP servers SHALL be written in one combined mutation. Agent Marketplace
SHALL NOT write trust state, vendor-managed policy, or private runtime
plugin caches. Claude Code registration SHALL be written to
`.claude/settings.local.json` at repository scope and to
`~/.claude/settings.json` at user scope, and the Claude catalog SHALL be a
marketplace file Claude Code accepts, naming an owner and listing each
plugin with a source relative to the catalog root. Factory Droid
registration SHALL be written to `.factory/settings.local.json` at
repository scope and to `~/.factory/settings.json` at user scope, SHALL
declare the catalog with a `local` marketplace source, and the Factory
catalog SHALL be a `.factory-plugin/marketplace.json` marketplace naming an
owner and listing each plugin with a source relative to the catalog root.

#### Scenario: Claude registration preserves handwritten settings

- **WHEN** a plugin is delivered natively to Claude Code at repository scope,
  `.claude/settings.local.json` has handwritten `enabledPlugins` entries, and
  `.claude/settings.json` has owned hooks
- **THEN** `.claude/settings.local.json` MUST gain the owned registration
  entries and keep every handwritten entry and comment, and
  `.claude/settings.json` MUST keep the owned hooks and gain no registration
  entry

#### Scenario: Claude catalog is a marketplace Claude Code accepts

- **WHEN** a plugin is delivered natively to Claude Code
- **THEN** the owned catalog's `.claude-plugin/marketplace.json` MUST name the
  catalog and an owner and list the plugin with a source relative to the
  catalog root that resolves to the plugin's package generation

#### Scenario: Claude registration moves out of shared project settings

- **WHEN** an owned Claude registration from an earlier sync is recorded in
  `.claude/settings.json` and sync runs
- **THEN** after that one sync the owned entries MUST be in
  `.claude/settings.local.json` and gone from `.claude/settings.json`, the
  plugin MUST be registered exactly once, and entries without ownership state
  MUST be left unchanged

#### Scenario: Factory registration preserves handwritten settings

- **WHEN** a plugin is delivered natively to Factory Droid at repository
  scope and `.factory/settings.local.json` has handwritten `enabledPlugins`
  and `extraKnownMarketplaces` entries
- **THEN** `.factory/settings.local.json` MUST gain the owned
  `extraKnownMarketplaces` entry with a `local` source naming the catalog and
  the owned `enabledPlugins` entry for the plugin, MUST keep every handwritten
  entry and comment, and `.factory/settings.json` MUST gain no registration
  entry

#### Scenario: Factory catalog is a marketplace Droid accepts

- **WHEN** a plugin is delivered natively to Factory Droid
- **THEN** the owned catalog's `.factory-plugin/marketplace.json` MUST name
  the catalog and an owner and list the plugin with a source relative to the
  catalog root that resolves to the plugin's package generation

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

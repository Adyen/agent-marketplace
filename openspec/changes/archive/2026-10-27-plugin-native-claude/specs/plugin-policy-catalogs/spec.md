## ADDED Requirements

### Requirement: Claude Code native delivery is verified by its probe

The shipped Claude Code harness catalog SHALL declare native delivery
verified for the `claude-plugin` format with the `claude-native-plugin`
probe and a `restart-required` refresh. The probe SHALL run the installed
Claude CLI offline with an isolated home and SHALL require a natively
delivered fixture plugin to be absent before sync. After sync it SHALL start
one Claude session so Claude registers the declared catalog itself, then
require a separate Claude session to report the plugin enabled with its
component available, at user scope and at repository scope. Agent
Marketplace SHALL NOT write Claude's installed-plugin records, known
marketplace records, plugin cache, or trust for the probe to pass. A
failing, blocked, or skipped probe SHALL fail the harness validation job.

#### Scenario: Claude native plugin probe loads the registered plugin

- **WHEN** the Claude Code harness validation job syncs a `claude-plugin`
  fixture with `delivery: native` at user scope and at repository scope and
  starts one Claude session
- **THEN** the Claude CLI MUST report the fixture plugin absent before sync,
  and a later Claude session MUST report it enabled with its component
  available, at each scope, without Agent Marketplace writing Claude's plugin
  records or cache

#### Scenario: Shipped Claude catalog delivers claude-plugin packages natively

- **WHEN** a `claude-plugin` package with `delivery: auto` is synced to
  Claude Code with the shipped catalogs
- **THEN** it MUST be delivered natively and reported `reload-required`, and
  the catalog check MUST pass with the `claude-native-plugin` probe defined
  and fail without it

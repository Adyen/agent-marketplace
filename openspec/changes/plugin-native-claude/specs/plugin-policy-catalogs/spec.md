## ADDED Requirements

### Requirement: Claude Code native delivery is verified by its probe

The shipped Claude Code harness catalog SHALL declare native delivery
verified for the `claude-plugin` format with the `claude-native-plugin`
probe. The probe SHALL run the installed Claude CLI offline with an isolated
home and SHALL require a natively delivered fixture plugin to be absent
before sync and reported enabled, with its component available, after sync
at user scope and at repository scope. A failing, blocked, or skipped probe
SHALL fail the harness validation job.

#### Scenario: Claude native plugin probe loads the registered plugin

- **WHEN** the Claude Code harness validation job syncs a `claude-plugin`
  fixture with `delivery: native` at user scope and at repository scope
- **THEN** the Claude CLI MUST report the fixture plugin absent before sync,
  and enabled with its component available after sync, at each scope

#### Scenario: Shipped Claude catalog delivers claude-plugin packages natively

- **WHEN** a `claude-plugin` package with `delivery: auto` is synced to
  Claude Code with the shipped catalogs
- **THEN** it MUST be delivered natively, and the catalog check MUST pass
  with the `claude-native-plugin` probe defined and fail without it

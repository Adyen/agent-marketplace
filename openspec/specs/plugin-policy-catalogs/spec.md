# plugin-policy-catalogs Specification

## Purpose
Defines the shipped plugin policy catalogs: embedded per-harness and
per-package-format data that records which plugin package layouts each
harness accepts, whether decomposed and native plugin delivery are
verified, and the evidence behind each claim. Governs catalog validation
and the generated plugin support reference.

## Requirements

### Requirement: Plugin catalogs are shipped, validated data

Every registered harness target and every recognized package format SHALL
ship an embedded plugin catalog consumed by the offline catalog check.
Malformed catalog data MUST fail the check. Plugin delivery and
plugin-surface field handling SHALL be decided only from these catalogs.

#### Scenario: Every harness target and package format ships a plugin catalog

- **WHEN** the catalog check loads the shipped plugin catalogs
- **THEN** every registered harness target and every recognized package
  format MUST have exactly one

#### Scenario: Malformed plugin catalogs fail the check

- **WHEN** a plugin catalog carries an unknown key, a duplicate key, a field
  outcome outside the allowed set, a blocking outcome without a diagnostic
  code, an unknown policy or format reference, or no evidence refs
- **THEN** the catalog check MUST fail and name the file and field

### Requirement: Native delivery is declared only with probe evidence

A harness catalog SHALL declare native delivery as verified only together
with the identifier of a native probe present in the harness validation
suite, and only for a package format the harness accepts.

#### Scenario: Verified native delivery without a probe fails the check

- **WHEN** a harness catalog declares native delivery verified with no probe
  identifier, or with a probe identifier the harness validation suite does
  not define
- **THEN** the catalog check MUST fail

#### Scenario: Verified native delivery for an unaccepted format fails the check

- **WHEN** a harness catalog declares native delivery verified for a package
  format the same catalog does not accept
- **THEN** the catalog check MUST fail

### Requirement: The plugin support reference is generated from the catalogs

The public plugin support reference SHALL be generated from the shipped
plugin catalogs and SHALL list, per harness target, the accepted package
formats, the decomposed and native delivery status, and the evidence date.
The catalog check MUST fail when the committed reference is stale.

#### Scenario: Stale plugin support reference fails the check

- **WHEN** a plugin catalog changes and the committed reference is not
  regenerated
- **THEN** the catalog check MUST fail

### Requirement: Conversion fixtures prove plugin catalog policies

The catalog check SHALL run hand-written plugin conversion fixtures through
the public package compilation path, and each fixture's expected projection
and diagnostics MUST match.

#### Scenario: A plugin fixture mismatch fails the check

- **WHEN** a plugin catalog edit changes the projection of a member covered
  by a conversion fixture
- **THEN** the catalog check MUST fail and report the fixture

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

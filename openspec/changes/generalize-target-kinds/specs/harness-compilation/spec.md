## ADDED Requirements

### Requirement: Compiled harness targets declare accepted resource kinds

Every compiled harness target definition SHALL declare its accepted resource kinds: skill, command, subagent, hook, MCP server and plugin for every current target, plus rule exactly for the targets that emit rule artifacts (Factory, Cursor and Windsurf).

#### Scenario: Compiled targets declare the observed kind matrix

- **WHEN** the compiled harness target definitions are read
- **THEN** every definition MUST declare skill, command, subagent, hook, MCP server and plugin
- **AND** exactly Factory, Cursor and Windsurf MUST also declare rule

### Requirement: Target completeness follows declared kinds

Target validation MUST fail when a declared accepted kind lacks a compiler, ingester or catalog registration that the kind requires, and MUST NOT require registrations for kinds the target does not declare.

#### Scenario: Declared kind without an implementation fails validation

- **WHEN** a compiled target definition declares an accepted kind whose required registration is missing
- **THEN** target validation MUST fail naming the target and the kind
- **AND** a target that does not declare that kind MUST pass validation without the registration

### Requirement: Plugins compile only as packages

A plugin MUST compile only through the package entry point. Compiling a plugin as a leaf resource MUST keep refusing with `plugin.compile.package-required`.

#### Scenario: Plugins compile only as packages

- **WHEN** a plugin resource is compiled as a leaf resource for any compiled harness target
- **THEN** compilation MUST refuse with `plugin.compile.package-required`
- **AND** compiling the same plugin through the package entry point MUST produce the existing package projection

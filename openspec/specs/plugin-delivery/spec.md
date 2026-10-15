# plugin-delivery Specification

## Purpose
Defines how plugin members are delivered to a harness target under the
plugin-surface field policies in the `pluginpolicy` catalogs: fields the
catalog marks as blocking restrictions refuse lossy delivery, while
preference losses omit only the field with a warning.

## Requirements

### Requirement: Security restrictions are never accepted as loss

A member field that the harness catalog marks as a blocking restriction on
the plugin surface SHALL block the member with the catalog's diagnostic. No
option SHALL install that member without the restriction. A preference loss
(for example an unresolved model) SHALL omit only the field, with a warning.

#### Scenario: Claude plugin subagent with permissionMode is blocked

- **WHEN** a plugin subagent declaring `permissionMode` is delivered natively
  to Claude Code
- **THEN** the member MUST be blocked with
  `plugin.member.restriction-unsupported`, blocking the instance

#### Scenario: Unresolved model on a plugin member warns and installs

- **WHEN** a plugin subagent selects a model the destination model catalog
  cannot resolve
- **THEN** the member MUST be delivered without the model field, with a
  warning

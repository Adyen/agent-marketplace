# Proposal

## Why

Every harness target is assumed to accept every resource kind. Completeness checks, catalogs and per-kind golden tests require each compiled target to implement all kinds, so a destination that accepts only some kinds cannot be described. Application targets that receive a subset of kinds need an explicit, declared accepted-kind set before any such destination can be added.

## What Changes

- Each compiled harness target declares the resource kinds it accepts. Every current harness target declares skill, command, subagent, hook, MCP server and plugin; rule is declared only by Factory, Cursor and Windsurf, which emit rule artifacts.
- One application-target registry composes compiled harness targets with their installation adapters and answers lookup, listing and kind acceptance. Accepted kinds are derived from the compiled declarations rather than kept in a separate list.
- Completeness validation requires an implementation only for each declared accepted kind. A declared kind without an implementation fails validation.
- Delivering a kind that a target does not accept refuses before compilation or writes, naming the kind and the target, and never falls back to another kind.
- Plugins remain package projections. Collections remain virtual selection expansion and are never compiled.
- Existing harness output, catalogs and diagnostics (including rule diagnostics on targets without rule artifacts) are unchanged.

No new harness target, target profile, `targets:` key, or place/register delivery is introduced.

## Capabilities

### New Capabilities
- `application-target-model`: application targets accept only declared kinds; current harness output and diagnostics are preserved.

### Modified Capabilities
- `harness-compilation`: compiled harness targets declare accepted resource kinds; completeness follows the declaration; plugins compile only through the package entry point.

## Impact

Harness target declarations in `pkg/harness-compilation`, the installation adapter registry in `internal/harness`, completeness and catalog tests, and the aggregate/golden test inventories. No configuration, lock, API, artifact or native output format change.

## Release note

none — internal target model change; installed output and diagnostics are unchanged.

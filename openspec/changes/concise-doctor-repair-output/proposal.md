## Why

A person deciding whether to repair two skill caches and adopt shared entries must read a machine inventory and many empty inspections before finding the confirmation. The ownership preview can say `nothing to repair` while drift is still pending; the embedded sync can say `Installed 0` even when cache contents changed.

## What Changes

- Lead human repair output with the proposed changes and their targets. Move healthy inventory, routine diagnostic detail and empty inspections to `doctor --repair --verbose`.
- Distinguish completed, remaining, deferred and failed work. Report clean only from a complete, empty plan.
- Keep actionable errors, blockers, affected entries and destructive consent visible. Plain `doctor`, JSON and repair authority do not change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `doctor-repair-planning`: define compact and detailed human repair output and the whole-plan result.

## Impact

Human CLI output, doctor help, repair guide and command/E2E witnesses. No change to selection, acquisition locks, cache reconciliation or ownership state formats.

## Release note

`doctor --repair` now shows what needs changing and what remains after repair. Use `doctor --repair --verbose` for inspected paths, counts and diagnostic details.

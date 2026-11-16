# Proposal

## Why

The target-profile design leaves profile identity, package discovery and the binding between a host package's assessment and the profile that interprets it unresolved. Those choices decide what the Marketplace records, what clients verify online and offline, and how slash-named host kinds are spelled. Settle them before the place-delivery contract is frozen.

## What Changes

- Update the target-profile design with how built-in and external profiles supply revisions of one target ID, and what happens on ambiguity or removal.
- Fix package boundaries, import mapping, sidecar metadata, target-scoped kind encoding, locator and prerequisite parsing, and server assessment-profile selection.
- Propose exact authenticated identity fields binding package revision and delivery mapping to the full profile digest, with refusal cases, and the host-package analysis disclosure policy.
- Trace each proposal to existing specs, implementation seams and future acceptance witnesses.
- Leave production behavior, wire formats and normative specifications unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This design-only change sets `skip_specs: true`.

## Impact

Only `docs/plans/2026-11-13-target-profiles-design.md` and this planning record change. No production implementation, parser, schema, artifact, assessment, lock, API, CLI, TUI or materialized output changes.

## Release note

none - Internal design proposal only; supported behavior does not change.

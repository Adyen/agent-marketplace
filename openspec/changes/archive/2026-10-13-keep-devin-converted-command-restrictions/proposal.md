## Why

A Devin converted command skill and a command do not carry the same controls. Ingestion currently recovers a marked Devin skill as a command even when the skill declares a `permissions` policy, an `agent` or `subagent` execution selector, or non-user activation, and drops those controls with a warning. A Devin-to-Devin round trip therefore loses a deny policy and makes a model-only resource user-invocable. In the other direction, Devin compilation copies `agent` and `subagent` from command extension fields next to the mapped `allowed-tools` restriction, so unmodeled metadata can select a different execution agent for the converted command skill.

## What Changes

- Devin ingestion refuses a marked converted command skill that declares `permissions`, `agent`, `subagent`, or triggers other than exactly user-only, with the error diagnostic `ingest.devin.command-security-unsupported`. The refused resource produces no artifact on any harness target.
- Marked converted command skills with user-only activation and a representable `allowed-tools` restriction keep round-tripping as commands.
- Devin compilation never emits `agent` or `subagent` from command extension fields. It reports the existing `compile.canonical-field-overrides-extra` warning and keeps the mapped `allowed-tools` restriction.
- Ordinary Devin skills keep their native `agent` and `subagent` execution selectors.

## Capabilities

### Modified Capabilities

- `harness-compilation`: converted command skills keep their restrictions across Devin ingestion and compilation.

## Impact

Devin ingestion and Devin command compilation in `pkg/harness-compilation`, the diagnostic catalog, and the field matrix. A hand-written Devin skill that carries the command marker together with one of the refused controls is no longer ingested; removing the marker ingests it as a skill with its controls intact. No fixture executes a resource, and no other harness target changes.

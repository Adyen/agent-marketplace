## ADDED Requirements

### Requirement: Converted command skills keep their restrictions

Harness Compilation MUST NOT discard an authority-bearing control while converting between a Devin converted command skill and a command. When the Devin source layout marks a skill with `x-agent-marketplace-kind: command` and the skill declares a `permissions` policy, an `agent` or `subagent` execution selector, or triggers other than exactly `[user]`, ingestion MUST refuse the resource with the error diagnostic `ingest.devin.command-security-unsupported`, and the refused resource MUST produce no artifact on any harness target. A marked skill with user-only activation and a representable `allowed-tools` restriction MUST still be ingested as a command. Devin compilation of a command MUST NOT emit `agent` or `subagent` from command extension fields; it MUST report `compile.canonical-field-overrides-extra` and keep the mapped `allowed-tools` restriction. Ordinary Devin skills MUST keep their native execution selectors.

#### Scenario: Marked Devin skill with controls the command cannot hold is refused

- **WHEN** a Devin source layout skill is marked as a converted command skill and declares a permissions deny policy, model-only activation, or an agent or subagent selector, next to an unmarked sibling skill
- **THEN** conversion to Devin and to Claude MUST report the error `ingest.devin.command-security-unsupported`, MUST emit no artifact for the marked resource, and MUST still emit the sibling skill

#### Scenario: Plain converted command skill still round-trips

- **WHEN** a Devin converted command skill declares user-only activation and a `read` allowed-tools restriction
- **THEN** conversion from Devin to Devin MUST report no error and MUST emit the converted command skill with the command marker, user-only triggers and the `read` restriction

#### Scenario: Command extension fields cannot select a Devin execution agent

- **WHEN** a command carries an `allowed-tools` extension field together with `agent` and `subagent` extension fields
- **THEN** Devin compilation MUST emit the mapped `allowed-tools` restriction without an `agent` or `subagent` field and MUST report `compile.canonical-field-overrides-extra`

#### Scenario: Native Devin skills keep execution selectors

- **WHEN** an unmarked Devin skill declares `agent` and `subagent` execution selectors
- **THEN** conversion from Devin to Devin MUST report no error and MUST emit both selectors unchanged

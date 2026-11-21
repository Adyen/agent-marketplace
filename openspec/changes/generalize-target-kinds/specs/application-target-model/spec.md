## ADDED Requirements

### Requirement: Application targets deliver only accepted resource kinds

Every application target SHALL declare the resource kinds it accepts, and the CLI SHALL deliver a resource to a target only when the target accepts its kind. Delivering an unaccepted kind MUST refuse before compilation or any write, naming the kind and the target, and MUST NOT deliver the resource as another kind. A collection is virtual selection expansion and is never a delivered kind.

#### Scenario: Current harness targets accept every selectable kind

- **WHEN** the application targets are listed
- **THEN** each of Factory, Claude, Cursor, Windsurf, Gemini, Devin, Codex and OpenCode MUST accept skill, command, subagent, hook, MCP server and plugin resources
- **AND** no target MUST accept collection as a delivered kind

#### Scenario: Unaccepted kind is refused before output

- **WHEN** a resource of a kind that a target does not accept is delivered to that target
- **THEN** delivery MUST refuse with a diagnostic naming the kind and the target
- **AND** no file for that resource MUST be written for that target, and the resource MUST NOT be delivered as a skill or any other kind

### Requirement: Accepted-kind targets preserve existing harness output

Declaring accepted kinds SHALL NOT change materialized output, shared-file ownership, plugin delivery, or diagnostics for any current harness target. Rule resources on harness targets that emit no rule artifact MUST keep their existing diagnostic codes and produce no artifact.

#### Scenario: Existing harness output is unchanged

- **WHEN** the existing golden resource fixtures are compiled for every current harness target
- **THEN** each output tree MUST be byte-identical to the output before accepted kinds were declared

#### Scenario: Rule diagnostics keep their codes

- **WHEN** a rule resource is compiled for Claude, Gemini, Devin, Codex or OpenCode
- **THEN** no rule artifact MUST be produced
- **AND** the diagnostic MUST keep its existing code (`rule.claude.no-primitive`, `rule.gemini.no-primitive`, `rule.devin.unsupported`, `rule.codex.unsupported`, or `rule.opencode.unsupported`)

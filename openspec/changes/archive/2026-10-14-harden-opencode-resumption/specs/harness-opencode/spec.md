## ADDED Requirements

### Requirement: Translation respects supported tool and model mappings

OpenCode translation SHALL use supported native model identifiers or explicitly report unmappable canonical models without emitting known-invalid selections. Explicit tool mapping overrides SHALL be honored consistently in restrictive subagent policies and supported hook matchers, without granting additional tools.

#### Scenario: An unmappable canonical model is not emitted

- **WHEN** an OpenCode command or subagent selects a canonical model with no supported native mapping
- **THEN** translation SHALL report the model loss and omit the invalid model selection
- **AND** verified native identifiers and explicit model overrides SHALL remain intact

#### Scenario: A tool override applies consistently

- **WHEN** a consumer supplies a supported explicit tool mapping override
- **THEN** supported restrictive subagent policies and hook matchers SHALL use that mapping
- **AND** unsupported mappings SHALL fail closed rather than expand tool authority

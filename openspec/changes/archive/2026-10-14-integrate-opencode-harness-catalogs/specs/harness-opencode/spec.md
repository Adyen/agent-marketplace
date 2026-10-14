## MODIFIED Requirements

### Requirement: Translation respects supported tool and model mappings

OpenCode translation SHALL resolve command and subagent model selections through the shipped harness model catalogs. A canonical model with no OpenCode catalog identifier SHALL be omitted and reported with warning `model.unresolved` rather than emitted as a known-invalid selection. Native OpenCode model selections SHALL keep their spelling through model provenance and SHALL be resolved by catalog identity for other harness targets. Agent tool restrictions SHALL be translated to OpenCode permission keys: a key SHALL be emitted only when every native tool it governs lies within the requested capability set, and the emitted keys SHALL cover that whole set; otherwise the subagent SHALL be refused with severity-error `subagent.opencode.restriction-unsupported`. Explicit tool mapping overrides SHALL be honored consistently in restrictive subagent policies and supported hook matchers, without granting additional tools.

#### Scenario: An unmappable canonical model is not emitted

- **WHEN** an OpenCode command or subagent selects a canonical model with no OpenCode catalog identifier
- **THEN** translation SHALL report `model.unresolved` and omit the model selection
- **AND** catalog-mapped identifiers, native OpenCode selections, and explicit model overrides SHALL remain intact

#### Scenario: Native model selections keep their provenance

- **WHEN** an OpenCode subagent with a provider-qualified native model is ingested and compiled for OpenCode and for another harness target
- **THEN** the OpenCode output SHALL keep the native model unchanged
- **AND** the other harness target SHALL receive the catalog identifier for the same model identity or report `model.unresolved`

#### Scenario: A tool override applies consistently

- **WHEN** a consumer supplies a supported explicit tool mapping override
- **THEN** supported restrictive subagent policies and hook matchers SHALL use that mapping
- **AND** unsupported mappings SHALL fail closed rather than expand tool authority

#### Scenario: Agent restrictions follow permission granularity

- **WHEN** a subagent allows or denies a tool set that maps onto whole OpenCode permission keys, such as file read, edit, and write together
- **THEN** translation SHALL emit those permission keys with the authored action
- **AND** a set that only part of a permission key covers, such as write without edit, SHALL refuse the subagent with `subagent.opencode.restriction-unsupported` instead of emitting an unenforced or broader rule

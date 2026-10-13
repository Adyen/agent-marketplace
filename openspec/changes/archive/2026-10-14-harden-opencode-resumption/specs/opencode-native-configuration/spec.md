## ADDED Requirements

### Requirement: Identity inventory uses captured bounded configuration reads

Before publishing commands or subagents, OpenCode identity inventory SHALL read existing native configuration through the applicable captured filesystem boundary and shared aggregate size limit. Supported contained and user-owned aliases SHALL remain readable. Malformed, ambiguous, unsafe, or oversized configuration SHALL refuse potentially shadowed command and subagent definitions, preserve installed generations, and leave independently safe resources eligible.

#### Scenario: Unsafe identity inventory refuses potentially shadowed definitions

- **WHEN** native identity inventory encounters malformed, ambiguous, unsafe, or oversized configuration
- **THEN** command and subagent publication SHALL refuse with `materialize.opencode.aggregate-malformed`, preserving existing configuration and definitions
- **AND** safely independent skills SHALL remain eligible while supported configuration aliases SHALL still expose handwritten identity collisions

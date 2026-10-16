## ADDED Requirements

### Requirement: Resource coordinates carry the slash brand motif
Marketplace result rows SHALL render the existing literal `/` between the registry label and resource name in weight-800 logo green `#00D16A`. The slash SHALL remain part of the accessible resource coordinate and MUST NOT introduce whitespace or change the registry label, resource name, route, or truncation behavior.

#### Scenario: Resource row highlights its meaningful coordinate separator
- **WHEN** the Marketplace displays a resource result row
- **THEN** its coordinate remains accessible as `registry/name`
- **AND** the literal delimiter is displayed as a weight-800 `#00D16A` slash

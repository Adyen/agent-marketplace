## ADDED Requirements

### Requirement: Resource coordinates carry the slash brand motif
Marketplace result rows SHALL render the existing literal `/` between the registry label and resource name in weight-800 logo green `#00D16A`, with one authored visible space on each side. The accessible resource coordinate SHALL remain `registry/name` without spaces and the presentation MUST NOT change the registry label, resource name, route, or truncation behavior. Hover emphasis SHALL use one continuous line beneath the complete coordinate rather than separate text decorations around the spaced slash.

#### Scenario: Resource row highlights its meaningful coordinate separator
- **WHEN** the Marketplace displays a resource result row
- **THEN** its coordinate remains accessible as `registry/name`
- **AND** the literal delimiter is displayed as a weight-800 `#00D16A` slash with one visible character space on each side
- **AND** hovering the coordinate shows one continuous emphasis line across its full width

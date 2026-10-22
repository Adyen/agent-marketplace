## MODIFIED Requirements

### Requirement: Discoverable footer actions
The resource list footer SHALL show the command palette, help and quit hints at every supported width, alongside status segments and active operations; the palette hint MAY use a shorter label to fit. While errors are present, the error count and the errors, reload and quit hints SHALL take precedence, followed by the palette and help hints as space allows. The errors hint SHALL be offered while the errors view has an error, warning or discovery error to show, and only then, matching the palette's errors action; pending drift alone SHALL NOT offer it. Other segments SHALL be dropped whole, lowest priority first, when space runs out. While search input is focused, the footer SHALL show how to apply and cancel the query and how to quit at every supported width.

#### Scenario: Narrow footer with status
- **WHEN** the list renders at 50, 80 or 120 columns with warnings, Marketplace health segments or an active operation
- **THEN** the palette, help and quit hints remain visible and no hint is cut mid-word

#### Scenario: Errors hint only with diagnostics
- **WHEN** the list has pending drift but no error, warning or discovery error
- **THEN** the footer MUST NOT offer the errors hint, and the palette MUST NOT offer the errors action
- **AND** once a warning is present, both MUST offer it

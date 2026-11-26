## ADDED Requirements

### Requirement: Uniform footer shortcut key emphasis

Every active keyboard shortcut hint rendered in the resource list footer SHALL style its key token with the same emphasized key role and its description with the muted description role, rather than highlighting only floor hints (`:`, `?`, `q`) while dimming primary action hints (`/`, `space`, `s`, `H`, `E`, `v`, `i`, `r`, `e`). Priority admission order, narrow-width floor retention, search-input hints, and colorless readability SHALL remain unchanged.

#### Scenario: Primary and floor footer hints share key emphasis

- **WHEN** the resource list footer renders on a color-capable terminal with both primary action hints and floor hints (`:`, `?`, `q`) visible
- **THEN** primary action keys such as `/` and `space` MUST use the same emphasized key styling as `:`, `?`, and `q`
- **AND** hint descriptions MUST remain in the muted style while colorless terminals keep readable plain labels

#### Scenario: Narrow widths retain floor priority order

- **WHEN** the terminal narrows to 50×12 with status segments and active hints
- **THEN** the footer MUST continue to drop lower-priority hints whole while keeping `:`, `?`, and `q` visible within physical bounds

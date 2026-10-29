## MODIFIED Requirements

### Requirement: Multi-harness selection

Harness editing and initial configuration SHALL support selecting multiple supported harness target IDs. Switching global and project scopes SHALL load that scope's selection without writing; Space SHALL toggle the focused selection without writing; only explicit Enter SHALL save after non-empty validation. The running CLI MUST understand every harness target ID named in consumer configuration, including harness selections and per-target materialization settings. Unknown IDs SHALL remain configuration errors that gate configuration-dependent actions, not warnings or permission to use only the supported subset. Refusal SHALL NOT silently remove or rewrite unknown entries.

#### Scenario: Scope and selection without write
- **WHEN** a user switches scopes with g or p, toggles a harness with Space, and cancels
- **THEN** each scope's selections remain available and all configuration bytes stay unchanged

#### Scenario: Save selected harnesses
- **WHEN** a user explicitly submits a non-empty multi-selection of supported harness target IDs
- **THEN** the guarded write MUST save that selection and update only the intended scope

#### Scenario: Empty selection
- **WHEN** a user attempts to save an empty harness selection
- **THEN** the form refuses the save without modifying configuration bytes

#### Scenario: Unknown harness target IDs refuse configuration
- **WHEN** otherwise valid consumer configuration names an unknown harness target ID, including alongside supported IDs or in per-target materialization settings
- **THEN** the TUI MUST show a configuration error and block synchronization instead of treating the unknown entry as a warning or using only supported targets
- **AND** the refused new operation MUST preserve authored configuration, acquisition locks and existing installed resources without installing pending resources
- **AND** after the user explicitly repairs the configuration, reload and synchronization MUST work with supported targets

### Requirement: Harness choices return after resizing

The initial harness-selection and target-harness dialogs SHALL restore the complete supported option list when the terminal expands after a compact layout. Resizing SHALL preserve the cursor and selected supported harness target IDs.

#### Scenario: Harness options return after compact resize

- **WHEN** a user opens either harness dialog at a supported full size, selects supported harness target IDs, shrinks the terminal to a compact size, then expands it to the original size
- **THEN** all supported harness choices SHALL be visible with the same cursor and selected IDs

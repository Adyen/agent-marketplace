## ADDED Requirements

### Requirement: Compound status reads in plain words

A resource status that combines a state with a qualifier SHALL read "<State>, <qualifier in plain words>" with lower-case words and no parentheses or semicolons. The qualifier SHALL name the reason in plain words, for example "waiting for sync", "waiting for verification", or "by org".

#### Scenario: Compound status reads in plain words

- **WHEN** a resource is enabled but its sync is pending, is required by an organization, or is awaiting verification
- **THEN** its status MUST read "enabled, waiting for sync", "required by org", or "<state>, waiting for verification" respectively
- **AND** no status MUST contain parentheses or a semicolon

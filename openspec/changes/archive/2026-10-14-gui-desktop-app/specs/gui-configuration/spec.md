## Purpose

Defines the GUI's structured user-personal configuration forms, their parity with equivalent CLI edits, and how organization policy and values managed outside the GUI are handled.

## ADDED Requirements

### Requirement: User-personal Harness target form

The Harness target form SHALL edit only the user-personal target Harness set using the `session-interactions` selection semantics. Organization Harness targets SHALL be shown read-only.

#### Scenario: Save user Harness targets without losing unknown identifiers

- **WHEN** a user changes and explicitly saves a non-empty Harness target selection that includes an unknown selected identifier already present in configuration
- **THEN** the user-personal target set SHALL be updated and the unknown identifier SHALL be preserved
- **AND** the Consumer organization configuration SHALL remain unchanged

#### Scenario: Organization Harness targets are read-only

- **WHEN** a user views Harness targets supplied by Consumer organization configuration
- **THEN** those targets SHALL be visible without any GUI action to modify them

### Requirement: User-personal source form

The source form SHALL add and remove Marketplace endpoint registrations and Git or directory registry sources in the user-personal root. It SHALL NOT modify sources from Consumer organization configuration.

#### Scenario: Add and remove user sources

- **WHEN** a user adds or removes a Marketplace endpoint registration or Git/directory registry source
- **THEN** only the user-personal root SHALL be changed
- **AND** Consumer organization configuration SHALL remain unchanged

#### Scenario: Organization sources are read-only

- **WHEN** a user views sources supplied by Consumer organization configuration
- **THEN** those sources SHALL be visible without any GUI action to add, remove, or edit them

### Requirement: Marketplace endpoint allowlist enforcement

Adding a Marketplace endpoint registration SHALL be refused when the effective `allowed_marketplaces` allowlist blocks its URL.

#### Scenario: Blocked endpoint cannot be added

- **WHEN** a user attempts to add a Marketplace endpoint whose URL is blocked by the effective allowlist
- **THEN** the GUI SHALL refuse the registration
- **AND** the user-personal configuration SHALL remain unchanged

### Requirement: Configuration outside the GUI is preserved

For a GUI Harness target or source edit with an equivalent CLI mutation, applying the same edit to the same starting user-personal root SHALL produce the same configuration bytes as the CLI edit. For a source edit with no CLI equivalent, the effective configuration after the write SHALL differ from the configuration loaded before it only by the added or removed entry, and every included fragment file SHALL remain byte-identical. The GUI SHALL identify values outside its forms as “managed outside the GUI”.

#### Scenario: Harness target edit matches the CLI mutation

- **WHEN** an equivalent CLI Harness target edit and a GUI Harness target edit apply the same selection to copies of the same user-personal root
- **THEN** both resulting configuration files SHALL be byte-identical
- **AND** includes, per-entry Harness target narrowing, and other unmodeled values SHALL match the CLI result
- **AND** the GUI SHALL identify those values as “managed outside the GUI”

#### Scenario: Source edit matches an equivalent CLI mutation

- **WHEN** an equivalent CLI source mutation and a GUI source edit apply the same operation to copies of the same user-personal root
- **THEN** both resulting configuration files SHALL be byte-identical
- **AND** includes, per-entry Harness target narrowing, and other unmodeled values SHALL match the CLI result
- **AND** the GUI SHALL identify those values as “managed outside the GUI”

#### Scenario: Adding a Git or directory source alone changes only that entry

- **WHEN** a user adds a Git or directory registry source without installing a Resource
- **THEN** the effective configuration SHALL differ from its pre-write value only by the added source entry
- **AND** every included fragment file SHALL remain byte-identical
- **AND** values outside the GUI's forms SHALL be identified as “managed outside the GUI”

#### Scenario: Removing a Marketplace endpoint registration changes only that entry

- **WHEN** a user removes a Marketplace endpoint registration from the user-personal root
- **THEN** the effective configuration SHALL differ from its pre-write value only by the removed registration
- **AND** every included fragment file SHALL remain byte-identical
- **AND** values outside the GUI's forms SHALL be identified as “managed outside the GUI”

# gui-configuration Specification

## Purpose
Defines the GUI's structured user-personal configuration forms, their parity with equivalent CLI edits, and how organization policy and values managed outside the GUI are handled.

## Requirements

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

### Requirement: User-personal supported Harness target form

The Harness target form SHALL edit only the user-personal target Harness set using the `session-interactions` selection semantics. Organization Harness targets SHALL be shown read-only.

#### Scenario: Save user Harness targets

- **WHEN** a user changes and explicitly saves a non-empty selection of supported Harness target IDs
- **THEN** only the user-personal target set SHALL be updated
- **AND** the Consumer organization configuration SHALL remain unchanged

#### Scenario: Unknown Harness target IDs refuse GUI configuration

- **WHEN** effective consumer configuration names a Harness target ID the running client does not support, including alongside supported IDs or only unsupported IDs
- **THEN** the GUI SHALL show a configuration error naming the configuration file and SHALL refuse Harness target editing and synchronization
- **AND** the refusal SHALL leave authored configuration, acquisition locks, and installed resources unchanged
- **AND** after the user repairs the configuration outside the GUI, reload SHALL restore supported target editing and synchronization

#### Scenario: Organization Harness targets are read-only

- **WHEN** a user views Harness targets supplied by Consumer organization configuration
- **THEN** those targets SHALL be visible without any GUI action to modify them

### Requirement: Recoverable source addition failure

Adding a directory registry source whose path does not exist SHALL show an inline Location error inside the Add source section, SHALL NOT write configuration, and SHALL leave the workspace usable. The Location input SHALL have a danger border, `aria-invalid="true"`, and `aria-describedby` referencing the error.

#### Scenario: Nonexistent directory source is refused in the form

- **WHEN** a user adds a directory registry source whose path does not exist
- **THEN** an error MUST show directly below Location with a danger border and accessible error association on that input
- **AND** the user-personal root MUST remain unchanged
- **AND** resource rows, the toolbar, and Settings MUST still respond

### Requirement: Add-source errors stay in the Add source section

Validation and request errors from adding a source SHALL remain inside the Add source modal, not at the top of Settings. Field errors SHALL appear directly below their inputs with danger styling and accessible error associations. Errors without a field SHALL appear once below the form. Editing a field SHALL clear its error; a successful add SHALL clear all errors.

#### Scenario: Add-source request error renders in the section

- **WHEN** a user adds a source and the request fails without identifying a field
- **THEN** the error MUST show once below the form inside the Add source modal
- **AND** the top of Settings MUST NOT show that error

#### Scenario: Add-source error clears after a successful add

- **WHEN** an add-source error is showing and the user then adds a source successfully
- **THEN** the Add source modal MUST close and no longer show the error

#### Scenario: Editing a field clears its error

- **WHEN** a source input has a field error and the user edits that input
- **THEN** that field's error and invalid-input styling MUST clear

### Requirement: Source form input validity

The shared source form SHALL disable Add source until a directory location is non-blank, a Git location is accepted by the existing GUI add-source API/config location admission rules with a non-blank ref, or a Marketplace location is an absolute HTTP(S) URL. Client checks SHALL NOT reject locations accepted by that admission boundary, and SHALL NOT loosen existing server safety rules. Directory existence SHALL remain a server check. Field hints SHALL appear only after blur or an attempted submit.

#### Scenario: Invalid source input disables addition

- **WHEN** a directory location is blank or whitespace, a Git location is rejected by the existing GUI add-source API/config location admission rules or its ref is blank, or a Marketplace location is not an absolute HTTP(S) URL
- **THEN** Add source MUST remain disabled
- **AND** an untouched field MUST NOT show a validation hint while its first characters are typed
- **AND** an invalid field MUST show its hint after blur or an attempted submit

#### Scenario: Valid source input enables addition

- **WHEN** the user supplies valid input for a source kind and the form is otherwise editable
- **THEN** Add source MUST be enabled
- **AND** the client MUST accept Git locations accepted by the existing GUI add-source API/config location admission rules
- **AND** directory existence MUST NOT be checked by the client

### Requirement: Native directory source location picker

The Add source form in the native desktop app SHALL offer a keyboard-operable Browse control beside Location for directory registries only. Choosing a folder SHALL populate Location with its absolute path and use the same validation as typed input. Browse SHALL NOT submit the form or write configuration.

#### Scenario: Choosing a folder fills Location without adding a source
- **WHEN** a user activates Browse for folder and selects a directory
- **THEN** Location SHALL contain the selected absolute path and follow the normal Location validation and error-clearing behavior
- **AND** no source SHALL be added until the user activates Add source

#### Scenario: The dialog starts at an existing Location or home
- **WHEN** Browse opens the native folder dialog
- **THEN** it SHALL start at the current Location if it is an existing directory, or at the user's home directory otherwise

#### Scenario: Cancelling the dialog preserves Location
- **WHEN** the user dismisses the folder dialog without choosing a directory
- **THEN** Location SHALL remain unchanged and no source SHALL be added

#### Scenario: Browse is absent outside native directory source forms
- **WHEN** the selected kind is Git or Marketplace, or the form does not have a native directory picker binding
- **THEN** Browse SHALL be absent and typed Location entry SHALL remain available

### Requirement: Global settings are reported per field

The configuration payload SHALL report the effective user-personal settings for backup, logging, update check, materialization, and telemetry level. Each field SHALL report its value, its built-in default, the layer that sets it (user, organization, or default), and whether the organization locks it. Only an explicit organization telemetry level SHALL lock its field. Backup, logging, update check, and materialization SHALL NOT be locked: a user value wins, and an organization value is reported as the organization's default.

#### Scenario: Settings read reports the source of each field

- **WHEN** the user sets backup.max_days and the organization sets update_check.enabled to false
- **THEN** backup.maxDays MUST report source "user" with that value
- **AND** updateCheck.enabled MUST report source "organization" with value false and NOT be locked

#### Scenario: Organization telemetry level locks its field

- **WHEN** the organization sets telemetry.level
- **THEN** telemetry.level MUST report locked true with source "organization"

### Requirement: Save settings writes a partial user-personal patch

The configuration actions endpoint SHALL accept a `save-settings` action with a partial patch of backup, logging, update check, materialization, and telemetry level. An absent field SHALL be left unchanged, and a field sent as null SHALL remove its key. The save SHALL run as a guarded configuration operation that reports "Settings saved" on success and "Couldn't save settings" on failure, and SHALL leave every other key in the user-personal root unchanged.

#### Scenario: Save settings writes the changed keys

- **WHEN** the user saves backup.maxDays 5 and logging.level warn
- **THEN** the user-personal root MUST contain max_days 5 and level warn
- **AND** the operation MUST report a success notification
- **AND** the apiVersion and other keys MUST be unchanged

### Requirement: Locked and invalid settings are refused before any write

A save that sends telemetry.level while the organization locks it SHALL be refused with `localapi.setting_locked` and SHALL NOT write. A save with a value outside the documented range or set of choices SHALL be refused with `localapi.invalid_setting` and SHALL NOT write. In both cases the user-personal root SHALL keep its existing bytes.

#### Scenario: Save refuses a locked telemetry level

- **WHEN** the organization locks telemetry.level and the user saves telemetry.level
- **THEN** the save MUST be refused with `localapi.setting_locked`
- **AND** the user-personal root MUST keep its existing bytes

#### Scenario: Save refuses an invalid value without writing

- **WHEN** the user saves backup.maxDays -1
- **THEN** the save MUST be refused with `localapi.invalid_setting`
- **AND** the user-personal root MUST keep its existing bytes

### Requirement: Settings editor preserves the file

The v4 user-personal settings editor SHALL preserve comments and unrelated keys when it updates a section, SHALL remove a key whose value is cleared, and SHALL remove a section left empty. Its output SHALL validate with the v4 loader before the file is published.

#### Scenario: Editing a section keeps comments and unrelated keys

- **WHEN** a setting is changed in a user-personal root that has comments and other keys
- **THEN** the comments and other keys MUST remain unchanged

#### Scenario: Clearing the last key removes the section

- **WHEN** the only key in a settings section is cleared
- **THEN** the empty section MUST be removed

### Requirement: Settings tabs edit global settings

The Settings window SHALL show General (update checks and telemetry), Install mode, Backups, and Logging tabs, each with a Save changes and Discard footer. Save changes SHALL stay disabled until a field is changed and valid. A field the user has set SHALL offer a Reset action that sends null for that key. A field whose value is locked by the organization SHALL be disabled and SHALL say it is set by the organization. A field showing an organization or built-in default SHALL say so.

#### Scenario: General saves the update check

- **WHEN** the user turns off "Show when a new release is available" and selects Save changes
- **THEN** the save MUST send an update check patch with enabled false

#### Scenario: Locked telemetry is disabled with organization copy

- **WHEN** the organization locks the telemetry level
- **THEN** the telemetry choices MUST be disabled and the General tab MUST say the level is set by the organization

#### Scenario: Install mode saves the mode and overrides

- **WHEN** the user changes the install mode and selects a per-agent override
- **THEN** the save MUST send the materialization mode and the complete override map

#### Scenario: Backups tab blocks invalid day counts

- **WHEN** the user enters a negative number of days on the Backups tab
- **THEN** the field MUST show an inline message and Save changes MUST stay disabled

#### Scenario: Logging reset clears the user value

- **WHEN** the user resets the log level on the Logging tab and selects Save changes
- **THEN** the save MUST send the log level as null

#### Scenario: Settings save writes the user YAML

- **WHEN** the user changes a backup day count in the built app and selects Save changes
- **THEN** the user-personal root MUST contain the new value, and a "Settings saved" notification MUST appear

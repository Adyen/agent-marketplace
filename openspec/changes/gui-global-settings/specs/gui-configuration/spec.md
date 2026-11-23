## ADDED Requirements

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

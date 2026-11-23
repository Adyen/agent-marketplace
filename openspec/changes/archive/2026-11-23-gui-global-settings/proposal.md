## Why

Backup, logging, update check, materialization, and telemetry level are user-personal settings that the desktop app cannot show or change. Users edit them by hand or through the CLI, and the app does not say which layer sets each value, whether the organization locks it, or what the default is.

## What Changes

- The configuration payload reports effective user-personal settings. Each field carries its value, its built-in default, the layer that sets it (user, organization, or default), and whether the organization locks it.
- Only an explicit organization telemetry level locks a field. Backup, logging, update check, and materialization are never locked: the user value wins, and an organization value is reported as the organization's default.
- A new `save-settings` configuration action writes a partial patch to the user-personal root. A null field clears its key. Saves run as a guarded operation and report "Settings saved" or "Couldn't save settings".
- Saves that touch a locked field are refused with `localapi.setting_locked`. Invalid values are refused with `localapi.invalid_setting` before any write, and the file keeps its bytes.
- Telemetry writes reuse the existing telemetry editor, so the output stays byte-identical to the CLI.

## Capabilities

### Modified Capabilities

- `gui-configuration`: the configuration payload and configure actions cover global settings.

## Impact

`internal/config/settings_edit.go` (new), `internal/localapi/settings.go` (new), `internal/localapi/configuration.go`, `api/gui-local.yaml`, and the regenerated `gui/src/generated/api.d.ts`. The Settings UI follows in a later phase of this change.

## Release note

The desktop app's configuration now reports backup, logging, update check, materialization, and telemetry settings with their source. Settings can be saved from the desktop app, and the app refuses values that the organization locks or that are invalid.

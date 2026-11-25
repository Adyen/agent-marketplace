## Why

The Add source dialog should support keyboard submission and cancellation, and settings controls should make clear which values apply when backups or logging are disabled.

## What Changes

- Allow Enter to submit a valid Add source form, prevent invalid submission, and allow cancellation when no add is pending.
- Disable retention controls when backups are off and dependent logging controls when file logging is off, without clearing their values.

## Capabilities

### Modified Capabilities

- `gui-shell`: Add source keyboard submission and cancellation.
- `gui-configuration`: controls dependent on Backups and Logging.

## Impact

`gui/src/AddSourceModal.vue`, `gui/src/SourceForm.vue`, `gui/src/GlobalSettingsPanel.vue`, and their Vitest coverage.

## Release note

The Add source dialog now supports Enter and Cancel, and Settings makes it clearer which controls apply when backups or logging are off.

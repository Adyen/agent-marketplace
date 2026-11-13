## Why

The Add source form uses inconsistent field labels and stretches its action across the form. Invalid input can start a request, and field errors do not identify the input that needs correction.

## What Changes

- Use one sans field-label style while retaining the section heading style.
- Place field errors below their inputs with accessible error associations.
- Clear a field's error when it changes and all errors after a successful add.
- Keep Add source right-aligned at content width and disabled until the input is valid.
- Mirror the CLI's accepted Git locations without checking directory existence in the client.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `gui-configuration`: field-level error placement, recovery, and client validation in the shared source form.

## Impact

The shared Vue source form, its Settings error wiring, styles, component and browser tests, and embedded frontend assets change. The CLI parser and server-side source safety checks remain authoritative. There is no folder picker or client directory-existence check.

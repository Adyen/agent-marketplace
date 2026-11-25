## Why

Resource action names are title case ("Uninstall Resource") while the rest of the GUI uses sentence case. The inspector Scope value also shows the raw scope word `user`, which reads as an identifier.

## What Changes

- Resource action names and dialog titles use sentence case: "Install resource", "Uninstall resource", "Exclude resource", and "Restore resource".
- The inspector Scope value reads "User" for user scope, matching the "User sources" heading in Settings.

## Capabilities

### Modified Capabilities

- `gui-shell`: resource action names and the inspector Scope value.

## Impact

Resource action labels and dialog titles (`gui/src/WorkspaceView.vue`, `gui/src/Inspector.vue`, `gui/src/ResourceActions.vue`, `gui/src/grouping.ts`), their tests and Playwright specs, and DESIGN.md. No action behaviour changes.

## Release note

Resource action buttons and dialog titles use sentence case, and the inspector shows "User" as the scope of user resources.

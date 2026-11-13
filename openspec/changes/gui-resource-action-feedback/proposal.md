# Resource actions and named feedback

## Why

Plain installs need an unnecessary confirmation, and completion messages do not name what changed. Loading and modal layout also obscure the action in progress.

## What Changes

- Install and Restore confirm prepared plans without a dialog when no risk acceptance is required. Risk, preparation errors, Remove, and Exclude keep a dialog.
- Align modal titles and close buttons, show progress cursors during resource planning and confirmation, and prevent pointer cursors on disabled buttons.
- Name operation outcomes and resource progress in plain language. Deliver a typed notification outcome instead of inferring tone from text.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `gui-shell`: resource confirmation, modal headers, busy controls, and operation feedback.
- `gui-local-api`: named outcome notifications and their generated payload contract.

## Impact

Vue resource actions, inspector, shared modal, feedback rendering, local API operations, OpenAPI and generated client, GUI tests, and embedded frontend assets. Existing guarded writes and selection behavior remain unchanged. This change adds no install auto-sync.

## Release note

Plain Install and Restore actions no longer ask for confirmation unless a plan needs risk acceptance or cannot be prepared. Notifications now name the resource or setting that changed, and resource actions show clearer progress.

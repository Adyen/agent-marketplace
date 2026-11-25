## Why

Adding a source happened in two places: an inline form at the top of Settings → Sources, and the sidebar "+" that opened that same tab. The inline form made Settings a long mixed page, the sidebar "+" was not an action, and the add flow's outcome (pending, failed, or settled) was not visible where the user started it.

## What Changes

- A dedicated "Add source" modal reuses the shared source form (Directory registry, Git registry, Marketplace endpoint). The sidebar "+", the empty-workspace "Add a source" button, and a new "Add source" button in Settings → Sources all open it.
- The inline add form is removed from Settings → Sources. The source list and remove action stay there.
- The modal stays open with the primary button busy ("Adding…") while the add operation is pending. It closes only when the operation settles without an add-source failure. On failure it shows the mutation diagnostics inline and stays open.
- Settings does not repeat an add-source error that belongs to the modal's operation. Mutation errors from other operations keep their current place at the top of Settings.
- Opening the modal from Settings keeps Settings open beneath it; closing the modal returns focus to Settings.

## Capabilities

### Modified Capabilities

- `gui-shell`: sidebar source actions and the sidebar add control open the Add source modal.
- `gui-configuration`: add-source errors render in the Add source modal rather than an inline Settings section.

### New Capabilities

- None. The modal is specified as a `gui-shell` requirement.

## Impact

`gui/src/AddSourceModal.vue` (new), `WorkspaceView.vue`, `SettingsWindow.vue`, `SourceForm.vue`, `controller.ts` (add-source baseline), `gui/DESIGN.md`, the Vitest and Playwright sources, accessibility, and gui specs that drove the inline form, and first-run (which keeps its own inline form).

## Release note

Adding a source opens an "Add source" dialog from the sidebar "+", the empty workspace, or Settings → Sources. The dialog stays open while the source is being added and reports any error inside the dialog. Settings → Sources now lists and removes sources only.

## Why

Adding a source in the desktop GUI gave no view of what the source holds, required a Git ref that `install --from` does not, showed a "required" error when Browse was pressed on an empty field, and reported failures as raw internal errors. Adding any source also failed with a generation conflict whenever the organization configuration needed a transient Marketplace lock.

## What Changes

- The Add source dialog previews a Git or directory source before adding it: the resolved branch and commit, and the resources found.
- Resources can be turned on in the preview to install them as the source is added.
- A Git source may omit its ref; the repository's default branch is resolved and saved.
- Blank fields no longer show a validation error; the submit button stays disabled instead.
- Failures are summarised in plain words, with the original text under Details.
- A staged configuration edit no longer conflicts with transient organization Marketplace acquisition.

## Capabilities

### Modified Capabilities

- `gui-configuration`: optional Git ref, source preview endpoint, blank-field validation.
- `gui-shell`: Add source preview step and failure copy.
- `deployed-acquisition-locks`: transient acquisition with staged configuration edits.

## Impact

`internal/localapi` (new `POST /api/source-preview`, optional Git ref), `api/gui-local.yaml`, `internal/pipeline/acquisition.go`, `gui/src/AddSourceModal.vue`, `gui/src/SourceForm.vue`, and their tests.

## Release note

The desktop app's Add source dialog now previews the resources in a Git or directory source before adding it, lets you turn on the resources to install right away, uses the repository's default branch when no branch is given, and explains failures in plain words. Adding a source no longer fails with a "generation conflict" when your organization configures a Marketplace.

## Why

Removal now refuses when a direct Marketplace pin shares its kind and name
with another explicit selection in the same scope. The remediation for the pin
suggested `--from <endpoint> --path <registry-id>`, but `--from` only accepted
Git and directory sources, so no command could remove the pin by itself.

## What Changes

- Named `remove --from <configured Marketplace URL>` removes that endpoint's
  matching direct pin or collection through the existing Marketplace removal.
- The ambiguity remediation for a Marketplace selection names only its
  endpoint and scope.
- Marketplace endpoint removal defaults to repository scope like other `--from`
  removals and refuses `--ref`, `--path`, missing selections, and URLs that
  also identify a Git or directory source.

## Capabilities

### Modified Capabilities

- `resource-removal`: remove Marketplace selections by configured endpoint.

## Impact

Shared removal pipeline, `remove --from` help and reference documentation,
pipeline regressions, and Marketplace E2E coverage. Git and directory `--from`
handling, configuration formats, and acquisition-lock formats are unchanged.

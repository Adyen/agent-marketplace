## Why

Selecting the same installed name from different sources in user and repository configuration can cause a repository sync to prune the user's machine-scope skill. A machine-scope Factory hook command can also change between user and repository syncs because its path depends on the directory from which sync runs. Both problems make `sync --check` disagree across scopes.

## What Changes

- Resolve resource name collisions independently within machine and repository install scopes. Preserve warnings and deterministic precedence for competing sources within one scope.
- Expand machine-scope Factory hook commands to the absolute machine root, while repository-scope hook commands remain relative to the repository root.
- Prove that both scopes retain their selections and that checks agree after either sync order.

## Capabilities

### New Capabilities

- `resource-selection`: selection and name-collision behavior at each install scope.
- `materialization`: stable machine-scope hook command paths.

## Impact

Resource selection, Factory hook materialization, focused resolver/materializer tests, command integration tests, and a declarative E2E case. Configuration and lock formats do not change.

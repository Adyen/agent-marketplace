## Why

The scenario "Singleton and no-name install behavior is retained" says single-name
and no-name `--from` output MUST remain unchanged. Since install defaults to user
scope, an unscoped install reports the user scope instead of the repository scope,
so a literal reading conflicts with "Install defaults to user scope".

## What Changes

- The scenario states that selection and output are unchanged apart from the
  target scope, which follows "Install defaults to user scope" or `--scope`.
- No code or behavior changes.

## Capabilities

### Modified Capabilities
- `resource-installation`: "Existing install modes remain compatible" clarifies that its singleton scenario does not fix the target scope.

### New Capabilities
- None.

## Impact

- Specification wording only.

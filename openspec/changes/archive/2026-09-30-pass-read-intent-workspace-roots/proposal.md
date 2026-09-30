## Why

Some configuration readers rediscover roots after other parts of a command establish a validated Workspace. A nested personal-local directory can therefore be read inconsistently, and an invalid local root can reach configuration loading without Workspace validation.

## What Changes

- Require configuration reads to use the repository and personal-local roots selected together by a validated Workspace.
- Reject missing repository roots before reading configuration rather than falling back to the working directory.
- Fail inspection, browsing, and cache garbage collection before reading configuration or changing local state when the personal-local root escapes the repository.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `workspace-resolution`: Require aligned roots for nested-directory browsing and inspection, and reject an escaping personal-local root before read or mutation.

## Impact

Configuration intent loading, CLI browsing and inspection, cache garbage collection, installation and removal paths that load effective configuration, and tests for nested directories and invalid roots. No configuration schema or Workspace containment policy changes.

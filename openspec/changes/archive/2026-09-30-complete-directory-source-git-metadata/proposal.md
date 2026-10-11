## Why

Directory-source discovery currently excludes Git metadata only for Claude source layouts. Other supported source layouts still report `skills/.git` as a malformed skill, and Windsurf can reject the entire bundle. A consistent user-visible contract requires all layouts to exclude the metadata while preserving authored-skill errors.

## What Changes

- Exclude `.git` skill-directory candidates in Factory, Cursor, Gemini, Codex, and Windsurf source layouts, including Factory's directory-symlink case.
- Verify all affected directory-source consumers still discover authored skills and retain their existing malformed-skill behavior; Windsurf must not abort solely because of Git metadata.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `directory-source-discovery`: Add cross-layout and Windsurf failure scenarios to the existing Git metadata requirement.

## Impact

Source-layout ingestion across supported harnesses and shared CLI/TUI discovery. No configuration schema, model catalog, lock, or materialization change.

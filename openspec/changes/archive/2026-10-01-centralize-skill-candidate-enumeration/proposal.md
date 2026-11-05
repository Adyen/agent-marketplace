## Why

Skill enumeration repeats the same Git metadata exclusion in six source layouts. A shared enumeration helper keeps that policy consistent without changing resource parsing or discovery behavior.

## What Changes

- Centralize exact-name `.git` exclusion in `ingest.ReadSkillEntries`.
- Route the six affected source layouts through the helper while preserving their diagnostics and symlink handling.
- Test enumeration controls and measure existing ingestion benchmarks.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. The `directory-source-discovery` requirements remain unchanged; this is a behavior-preserving refactor.

## Impact

Shared skill enumeration and the Claude, Factory, Cursor, Gemini, Windsurf, and Codex ingesters. No configuration, acquisition lock, or materialization changes.

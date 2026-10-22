## ADDED Requirements

### Requirement: Personal-local writes are git-ignored at once

A successful TUI write to the personal-local layer, whether a single toggle, a local exclude, or a section bulk toggle, SHALL ensure in the same operation that `.agents/config.local.yaml` and `.agents/lock.local.yaml` are git-ignored, as sync does, so the personal-local files never appear as untracked repository files while a sync is pending. Writes to other layers SHALL leave `.gitignore` unchanged.

#### Scenario: Personal-local writes are git-ignored at once

- **WHEN** a TUI toggle, local exclude or bulk toggle writes the personal-local layer in a repository without those entries in `.gitignore`
- **THEN** `.gitignore` MUST list `.agents/config.local.yaml` and `.agents/lock.local.yaml` before any sync runs
- **AND** a TUI write to the repository layer MUST NOT change `.gitignore`

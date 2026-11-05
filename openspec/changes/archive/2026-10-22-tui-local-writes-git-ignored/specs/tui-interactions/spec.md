## ADDED Requirements

### Requirement: Personal-local writes are git-ignored at once

A successful TUI write to the personal-local layer, whether a single toggle, a local exclude, or a section bulk toggle, SHALL apply in the same operation the git-ignore step that sync applies, adding `.agents/config.local.yaml` and `.agents/lock.local.yaml` to `.gitignore`, so the personal-local files do not appear as untracked repository files while a sync is pending. Like sync, the step SHALL leave an unsafe `.gitignore`, such as a symbolic link, untouched and log the refusal instead of failing the write. Writes to other layers SHALL leave `.gitignore` unchanged.

#### Scenario: Personal-local writes are git-ignored at once

- **WHEN** a TUI toggle, local exclude or bulk toggle writes the personal-local layer in a repository without those entries in `.gitignore`
- **THEN** `.gitignore` MUST list `.agents/config.local.yaml` and `.agents/lock.local.yaml` before any sync runs
- **AND** a TUI write to the repository layer MUST NOT change `.gitignore`

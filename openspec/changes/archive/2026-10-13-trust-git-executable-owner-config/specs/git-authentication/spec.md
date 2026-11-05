## MODIFIED Requirements

### Requirement: Every included authentication file is trusted and bounded

The reader MUST validate each opened configuration file before reading its contents. Files MUST be regular, singly linked, and not writable by group or others. Each file MUST be owned by root, by the effective user, or by the owner of the `git` executable that acquisition runs: the `PATH` lookup, resolved through symlinks. The executable owner is trusted only when the lookup yields an absolute path and that executable is a regular file that is not writable by group or others. The reader MUST parse the same validated bytes rather than reopening their path. It MUST reject malformed configuration, include cycles, nesting beyond ten include levels, any file exceeding 4 MiB, expanded content exceeding 16 MiB, and more than 128 expanded file visits with `git.auth-config-untrusted` before acquisition transport runs. Missing optional configuration roots and include targets, and explicit `/dev/null` includes, SHALL contribute no settings.

#### Scenario: Unsafe include chains fail before transport

- **WHEN** an ordinary include chain contains an unsafe file, malformed configuration, a cycle, excessive depth, or excessive content
- **THEN** registry acquisition MUST return `git.auth-config-untrusted` before contacting the registry or executing a credential helper
- **AND** configuration and registry input bytes MUST remain unchanged
- **AND** the diagnostic MUST NOT expose authentication values or parser stderr

#### Scenario: Missing optional includes contribute no authentication

- **WHEN** an otherwise trusted chain references a missing optional include or `/dev/null`
- **THEN** the reader MUST continue with the remaining declared settings in order

#### Scenario: Configuration owned by the Git executable owner is trusted

- **WHEN** Git's system configuration belongs to neither root nor the current user, but to the account that owns the `git` executable on `PATH`, reached through a symlink as in a package-manager installation
- **THEN** authenticated registry acquisition MUST use that configuration and succeed

#### Scenario: Executable owner trust requires a protected executable

- **WHEN** the `git` executable is writable by group or others, is found through a relative `PATH` entry, or belongs to an account other than the configuration file's owner
- **THEN** acquisition MUST refuse the configuration with `git.auth-config-untrusted` before contacting the registry

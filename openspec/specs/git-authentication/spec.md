# git-authentication Specification

## Purpose
Defines trusted system/global Git authentication include chains for HTTP(S) registry acquisition, their resource bounds, and the settings that remain outside their authority.

## Requirements

### Requirement: Ordinary Git includes retain trusted authentication

HTTP(S) registry acquisition SHALL expand ordinary `include.path` entries from system and global Git configuration recursively. It MUST preserve declaration order, system-before-global precedence, XDG-before-home precedence, URL-specific authentication keys, and empty credential-helper resets. Absolute paths, `~/` paths, and paths relative to the declaring file MUST work. Symlinked regular configuration files MUST preserve the authored file's relative-include directory.

#### Scenario: Included authentication reaches clone and fetch

- **WHEN** trusted system or global Git configuration supplies authentication and TLS settings through ordinary nested includes
- **THEN** exact and legacy registry clone and subsequent fetch MUST authenticate successfully
- **AND** the configuration files MUST remain unchanged
- **AND** authentication settings MUST NOT persist in registry cache configuration

#### Scenario: Included helper order and resets are preserved

- **WHEN** authentication settings and ordinary includes are interleaved across the supported system and global roots
- **THEN** copied authentication values MUST retain Git's declaration order, URL-specific keys, and empty credential-helper resets
- **AND** repeated includes on independent branches MUST expand independently

### Requirement: Every included authentication file is trusted and bounded

The reader MUST validate each opened configuration file before reading its contents. Files MUST be regular, singly linked, owned by root or the effective user, and not writable by group or others. The reader MUST parse the same validated bytes rather than reopening their path. It MUST reject malformed configuration, include cycles, nesting beyond ten include levels, any file exceeding 4 MiB, expanded content exceeding 16 MiB, and more than 128 expanded file visits with `git.auth-config-untrusted` before acquisition transport runs. Missing optional configuration roots and include targets, and explicit `/dev/null` includes, SHALL contribute no settings.

#### Scenario: Unsafe include chains fail before transport

- **WHEN** an ordinary include chain contains an unsafe file, malformed configuration, a cycle, excessive depth, or excessive content
- **THEN** registry acquisition MUST return `git.auth-config-untrusted` before contacting the registry or executing a credential helper
- **AND** configuration and registry input bytes MUST remain unchanged
- **AND** the diagnostic MUST NOT expose authentication values or parser stderr

#### Scenario: Missing optional includes contribute no authentication

- **WHEN** an otherwise trusted chain references a missing optional include or `/dev/null`
- **THEN** the reader MUST continue with the remaining declared settings in order

### Requirement: Include support does not broaden authentication authority

Only the existing credential and TLS allowlist SHALL reach HTTP(S) acquisition subprocesses. Unrelated execution settings and TLS verification disabling MUST NOT become active through an include. Conditional `includeIf` entries MUST remain unsupported and return `git.auth-config-untrusted`. Repository-local credential, HTTP, include, askpass, and worktree-configuration settings MUST remain refused independently of ordinary system/global include support.

#### Scenario: Conditional includes remain explicitly refused

- **WHEN** a system or global chain contains a conditional include
- **THEN** acquisition MUST refuse it with `git.auth-config-untrusted`, including conditions that do not match

#### Scenario: Included settings do not activate unrelated Git behavior

- **WHEN** an ordinary trusted include also declares hooks, URL rewrites, or TLS verification disabling
- **THEN** only allowlisted authentication settings MUST be applied

#### Scenario: Worktree authentication does not gain authority

- **WHEN** acquisition runs from a caller worktree with repository-local authentication settings
- **THEN** those caller settings MUST NOT supply acquisition authentication
- **AND** authentication settings in the registry cache's repository-local or worktree configuration MUST remain refused

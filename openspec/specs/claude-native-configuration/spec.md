# claude-native-configuration Specification

## Purpose
Define Claude Code's effective machine configuration directory, preservation of shared account state, and separation from repository-scope output.

## Requirements

### Requirement: Claude Code machine destinations follow CLAUDE_CONFIG_DIR

When `CLAUDE_CONFIG_DIR` is set to an absolute path, Agent Marketplace SHALL use that directory as the Claude Code machine root in place of `~/.claude`. Machine-scope skills, commands, subagents, hook support, `.claude-plugin/plugin.json`, the native `settings.json` hook file and its legacy `hooks/hooks.json` SHALL live under that directory, and machine-scope MCP servers SHALL be reconciled in the `mcpServers` member of `$CLAUDE_CONFIG_DIR/.claude.json` in place of `~/.claude.json`. A missing directory SHALL be created on the first machine-scope write, in both materialization modes.

While the variable is set, `sync`, `sync --check`, `install`, `remove`, `update`, `status`, `doctor`, `backup`, `backup restore`, and harness detection SHALL resolve the same relocated root. Agent Marketplace SHALL NOT fall back to `~/.claude` or `~/.claude.json` as machine destinations. Previously generated output at default locations not themselves selected as effective destinations SHALL be left in place rather than read, moved, or pruned, even when the relocated root contains those locations.

A relative value SHALL refuse machine-scope Claude Code work and the `status` and `doctor` root summaries with an error naming `CLAUDE_CONFIG_DIR` as required to be absolute, before any machine-scope Claude Code write; repository-only Claude Code output and other harness targets SHALL be unaffected by that refusal. An unset or empty variable SHALL keep `~/.claude` and `~/.claude.json`. Repository-scope destinations, `<repo>/.claude` and `<repo>/.mcp.json`, SHALL NOT change.

#### Scenario: Machine selections install under the configuration directory

- **WHEN** `CLAUDE_CONFIG_DIR` is an absolute path distinct from the default destinations, `~/.claude.json` exists with unrelated members, the relocated configuration is missing or also contains unrelated members, and machine-scope selections include a skill, a command, a subagent, a hook, and an MCP server for Claude Code
- **THEN** `sync` MUST install the skill, command, subagent, hook support, `.claude-plugin/plugin.json`, and `settings.json` hook entries under `$CLAUDE_CONFIG_DIR`, in both materialization modes, creating the directory when it is missing
- **AND** the MCP server MUST appear under `mcpServers` in `$CLAUDE_CONFIG_DIR/.claude.json` with that file's unrelated members, handwritten entries, formatting, and permissions preserved
- **AND** `~/.claude` MUST NOT be created and the bytes of `~/.claude.json` MUST be unchanged
- **AND** `sync --check` MUST report no drift afterwards

#### Scenario: Relative value refuses machine-scope work before any write

- **WHEN** `CLAUDE_CONFIG_DIR` is set to a relative path and machine-scope selections include Claude Code resources
- **THEN** `sync`, `sync --check`, `status`, and `doctor` MUST exit nonzero with an error naming `CLAUDE_CONFIG_DIR` as required to be absolute
- **AND** no machine-scope Claude Code file MUST be written, created, or pruned, under `~/.claude`, `~/.claude.json`, or the relative path
- **AND** repository-scope Claude Code selections and selected resources for other harness targets MUST still install on `sync`

#### Scenario: Relative value does not affect repository-only output

- **WHEN** `CLAUDE_CONFIG_DIR` is set to a relative path and only repository-scope selections include Claude Code resources
- **THEN** `sync` MUST install them under `<repo>/.claude` and `<repo>/.mcp.json` without an error diagnostic

#### Scenario: Unset or empty variable keeps the default roots

- **WHEN** `CLAUDE_CONFIG_DIR` is unset or set to the empty string
- **THEN** machine-scope Claude Code output MUST go to `~/.claude` and `~/.claude.json` exactly as before
- **AND** no path under any other directory MUST be created for Claude Code machine output

#### Scenario: Repository scope ignores the variable

- **WHEN** `CLAUDE_CONFIG_DIR` is set and repository-scope selections include a skill and an MCP server for Claude Code
- **THEN** `sync` MUST write them to `<repo>/.claude` and `<repo>/.mcp.json`
- **AND** nothing for those selections MUST be written under `$CLAUDE_CONFIG_DIR`

#### Scenario: Default-location output is left in place

- **WHEN** `~/.claude` and `~/.claude.json` hold output generated before `CLAUDE_CONFIG_DIR` was set, and `sync` then runs with the variable selecting different resource and MCP destinations
- **THEN** `sync` MUST NOT modify, prune, or move any file under `~/.claude` or any entry of `~/.claude.json`
- **AND** `sync --check` MUST NOT report that output as drift

#### Scenario: Status, doctor, and backup resolve the relocated root

- **WHEN** `CLAUDE_CONFIG_DIR` is set and machine-scope Claude Code output has been synchronized
- **THEN** `status` and `doctor` MUST show `$CLAUDE_CONFIG_DIR` as the Claude Code machine root and MUST NOT report `~/.claude` or `~/.claude.json`
- **AND** `backup` MUST archive the relocated components and `$CLAUDE_CONFIG_DIR/.claude.json`, and `backup restore` MUST publish them into `$CLAUDE_CONFIG_DIR` without touching `~/.claude` or `~/.claude.json`

#### Scenario: Harness detection recognizes the configuration directory

- **WHEN** `CLAUDE_CONFIG_DIR` names an existing directory and `~/.claude` does not exist
- **THEN** `init` harness detection and the TUI first-run harness detection MUST pre-select Claude Code
- **AND** when `CLAUDE_CONFIG_DIR` names a missing directory, Claude Code MUST NOT be detected even if `~/.claude` exists

### Requirement: The Claude configuration file inside the machine root is reconciled, never pruned

When `.claude.json` lives inside the Claude Code machine root because `CLAUDE_CONFIG_DIR` is set, Agent Marketplace SHALL treat it exactly as it treats `~/.claude.json`: only owned `mcpServers` entries are added, updated, or removed; unrelated members, handwritten entries, comments, key order, and permissions are preserved; an unmanaged entry with a selected installed name fails closed; and a malformed file refuses only the MCP aggregate. Cleanup of the machine root, pruning of stale output, and removal of the last selected MCP server SHALL NOT delete or strip unrelated members from `.claude.json`. Explicit `backup restore` SHALL retain its existing raw-file overlay behavior, using the effective relocated destination rather than the default account.

#### Scenario: Pruning the relocated root preserves the configuration file

- **WHEN** `CLAUDE_CONFIG_DIR` is set, `$CLAUDE_CONFIG_DIR/.claude.json` holds unrelated members and a handwritten `mcpServers` entry, and `sync` runs with stale machine-scope Claude Code output to prune and no MCP server selected
- **THEN** `sync` MUST prune the stale output and leave the bytes of `.claude.json` unchanged

#### Scenario: Removing the last MCP server keeps the configuration file

- **WHEN** `CLAUDE_CONFIG_DIR` is set and `remove` deselects the only machine-scope MCP server for Claude Code
- **THEN** `sync` MUST remove that owned entry from `$CLAUDE_CONFIG_DIR/.claude.json` and keep the file with its unrelated members and handwritten entries intact

#### Scenario: Unmanaged collision in the relocated file fails closed

- **WHEN** `CLAUDE_CONFIG_DIR` is set and `$CLAUDE_CONFIG_DIR/.claude.json` already holds a handwritten `mcpServers` entry whose name matches a selected MCP server
- **THEN** `sync` MUST report the collision and leave that entry and the file's bytes unchanged
- **AND** other selected Claude Code resources MUST still install

### Requirement: Overlapping Claude Code scopes refuse before mutation

When `CLAUDE_CONFIG_DIR` is set and both repository and machine scope produce Claude Code output, Agent Marketplace SHALL compare the relocated machine root with the repository Claude Code root `<repo>/.claude` before any Claude Code write. When the two name the same location or one contains the other — including spellings that differ only in letter case or Unicode normalization, whether or not the directories exist yet — materialization MUST refuse with `materialize.destination.overlap` naming both paths and MUST NOT write, prune, or create anything for Claude Code in either scope. `sync --check` MUST report the same refusal and exit nonzero. Other selected harness targets SHALL continue. Disjoint roots SHALL support both scopes normally, and an unset variable SHALL retain the existing behavior when the repository root is the home directory.

#### Scenario: Configuration directory inside or around the repository root refuses

- **WHEN** `CLAUDE_CONFIG_DIR` names `<repo>/.claude`, a directory containing it such as `<repo>`, or a directory inside it, and both repository and machine scope select Claude Code output
- **THEN** `sync` and `sync --check` MUST report `materialize.destination.overlap` naming both paths and exit nonzero
- **AND** no Claude Code file in either scope MUST be written, created, or pruned
- **AND** selected resources for other harness targets MUST still install

#### Scenario: Configuration directory that aliases the repository root refuses

- **WHEN** `CLAUDE_CONFIG_DIR` spells `<repo>/.claude` with different letter case or a different Unicode normalization form, whether or not that directory exists yet, and both scopes select Claude Code output
- **THEN** `sync` MUST refuse with `materialize.destination.overlap` naming both spellings, on case-sensitive and case-insensitive hosts alike
- **AND** neither spelling MUST receive any write

#### Scenario: Disjoint configuration directory supports both scopes

- **WHEN** `CLAUDE_CONFIG_DIR` names a directory outside the repository and both scopes select Claude Code output
- **THEN** `sync` MUST install repository selections under `<repo>/.claude` and `<repo>/.mcp.json` and machine selections under `$CLAUDE_CONFIG_DIR` and `$CLAUDE_CONFIG_DIR/.claude.json` without an error diagnostic

### Requirement: Relocated Claude operations retain native output boundaries

When `CLAUDE_CONFIG_DIR` is set, marker-based cleanup SHALL inspect only aggregate paths declared for the Claude Code harness target, through admitted parent and regular-file authority. A marker in another file beneath that directory SHALL NOT by itself authorize Claude cleanup to read, rewrite, or prune that file. Empty component plans SHALL retain this restriction while still pruning actual stale Claude output and preserving shared permissions. An admitted machine-root directory alias SHALL support native hook configuration and support publication in both materialization modes, retaining the existing source, destination, ownership and generation checks.

Root-overlap comparison SHALL recognize Unicode case variants as well as canonical normalization variants, including prospective paths whose case relationship cannot yet be established through existing filesystem objects.

#### Scenario: Relocated cleanup ignores unrelated files

- **WHEN** a relocated Claude Code root contains stale selected output and unrelated JSON files, including another account's configuration or another repository's MCP configuration, whose entries carry Agent Marketplace markers
- **THEN** synchronization MUST prune the stale Claude resource and aggregate entries while preserving unrelated bytes, inode, permissions and modification time in both materialization modes

#### Scenario: Admitted root alias supports native hooks

- **WHEN** `CLAUDE_CONFIG_DIR` names an admitted directory alias and machine-scope selections include native Claude Code hooks and MCP servers
- **THEN** synchronization MUST install native hook settings, support and MCP entries through that alias in both materialization modes without weakening filesystem boundary checks
- **AND** a subsequent read-only synchronization check MUST report no drift

#### Scenario: Relocated cleanup preserves an unsafe aggregate parent

- **WHEN** a declared aggregate's admitted component directory is retargeted to an unrelated directory during stale-output pruning
- **THEN** synchronization MUST refuse the unsafe path before reading or rewriting the foreign aggregate, preserving its bytes and metadata
- **AND** restoring the original component MUST permit actual stale-entry cleanup

#### Scenario: Prospective Unicode case variants refuse

- **WHEN** repository and machine Claude Code roots differ only by Unicode case variants such as uppercase sigma and final sigma, or ASCII S and long S, and the roots do not yet exist
- **THEN** destination preflight MUST refuse with `materialize.destination.overlap` before either root is created

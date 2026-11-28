## ADDED Requirements

### Requirement: Supported platforms

The agent-marketplace CLI SHALL support macOS (`darwin`), Linux (`linux`), and Windows (`windows`) on `amd64` and `arm64` processors. Each stable release SHALL publish one CLI binary per supported platform, named `agent-marketplace-<tag>-<os>-<arch>`, with the suffix `.exe` for Windows binaries.

#### Scenario: Release build produces Windows binaries

- **WHEN** the CLI release build runs for a stable tag
- **THEN** it produces `agent-marketplace-<tag>-windows-amd64.exe` and `agent-marketplace-<tag>-windows-arm64.exe` alongside the macOS and Linux binaries

#### Scenario: Windows binary reports its version

- **WHEN** a Windows release binary runs with `--version`
- **THEN** it exits 0 and prints the release version stamped at build time

### Requirement: Machine locations on Windows

On Windows, `~` in every documented location SHALL mean the current user's profile directory as reported by Windows (`%USERPROFILE%`). A `HOME` environment variable alone SHALL NOT change it. User configuration, user acquisition locks, backups, the registry cache, the default state root, and the machine roots of the Claude Code, Codex, Cursor, Factory, Gemini CLI, OpenCode, and Windsurf harness targets SHALL resolve under that directory with the same relative paths as on other platforms. Absolute `XDG_STATE_HOME`, `CODEX_HOME`, and `CLAUDE_CONFIG_DIR` values SHALL keep their existing override meaning.

#### Scenario: User layer resolves under the profile directory

- **WHEN** `install <name> --scope user` runs on Windows and `%USERPROFILE%\.agents` does not exist
- **THEN** the command creates `%USERPROFILE%\.agents\config.yaml` as a v4 root that selects the resource
- **AND** `sync --check` reports the user layer in sync

#### Scenario: HOME does not relocate machine locations

- **WHEN** a command runs on Windows with `HOME` set to a directory other than `%USERPROFILE%`
- **THEN** the user configuration is read from `%USERPROFILE%\.agents\config.yaml`
- **AND** no default location is resolved below the `HOME` directory, while explicit overrides such as an absolute `XDG_STATE_HOME` keep their meaning

### Requirement: Devin machine scope on Windows

On Windows, Agent Marketplace SHALL NOT materialize resources for the Devin harness target at machine scope. A machine-scope selection that targets Devin SHALL report the warning `harness.devin.windows-machine-scope-unsupported` once per run, write nothing under any Devin machine root, and leave other harness targets and repository-scope Devin output unaffected. Exit codes are unchanged by this warning.

#### Scenario: Machine-scope Devin selection is skipped with a warning

- **WHEN** sync runs on Windows with a user-layer selection that targets Devin and Claude Code
- **THEN** it reports `harness.devin.windows-machine-scope-unsupported` once
- **AND** nothing is written for Devin at machine scope
- **AND** the selection is materialized for Claude Code

### Requirement: Consumer organization configuration on Windows

On Windows, the consumer organization configuration and its acquisition lock SHALL be read from `agent-marketplace\config.yaml` and `agent-marketplace\lock.yaml` under the machine's program-data folder (by default `C:\ProgramData`). The `ProgramData` environment variable SHALL NOT change this location. Because ordinary users can create folders in the program-data folder, the configuration SHALL be applied only when the `agent-marketplace` directory, the configuration file, and every organization include and lock it uses are owned by SYSTEM, the Administrators group, or TrustedInstaller, and grant write, delete, change-permission, or take-ownership access to no other principal. Otherwise the configuration MUST NOT be applied, configuration-dependent actions MUST be refused, and the refusal MUST report the existing `organization.trust.ancestor.owner`, `organization.trust.ancestor.writable`, `organization.trust.<kind>.owner`, or `organization.trust.<kind>.writable` code naming the refused path, with a hint directing the user to their administrator.

#### Scenario: Organization configuration is read from the program-data folder

- **WHEN** an administrator-owned `C:\ProgramData\agent-marketplace\config.yaml` is a v4 root that selects a resource, and `sync` runs on Windows
- **THEN** the resource is materialized as an organization-layer selection
- **AND** `status` reports the organization layer from that path

#### Scenario: ProgramData environment variable does not redirect the organization layer

- **WHEN** a command runs on Windows with the `ProgramData` environment variable pointing at a user-writable directory that contains `agent-marketplace\config.yaml`
- **THEN** that file is not read
- **AND** the organization layer is resolved from the machine's program-data folder

#### Scenario: Organization directory created by an ordinary user is refused

- **WHEN** `C:\ProgramData\agent-marketplace` is owned by a non-administrative user and contains a v4 `config.yaml`
- **THEN** configuration-dependent commands exit non-zero with `organization.trust.ancestor.owner` naming that directory
- **AND** no resource selected by that file is materialized

### Requirement: Permission requirements on Windows

Where a requirement specifies owner-only access or a POSIX permission mode such as `0600` or `0700`, a file or directory created on Windows SHALL grant access only to the current user, SYSTEM, and the Administrators group. A check that refuses a file readable or writable by group or other SHALL, on Windows, refuse a file whose access control list grants that access to any other principal, and SHALL NOT refuse a file only because of the POSIX mode bits Windows reports.

#### Scenario: Private file created on Windows is owner-only

- **WHEN** a sync on Windows writes a backup archive under `%USERPROFILE%\.agent-marketplace\backups\agents-config\`
- **THEN** the archive grants access only to the current user, SYSTEM, and the Administrators group

#### Scenario: Owner-only file under the profile directory is accepted

- **WHEN** a telemetry credential file under `%USERPROFILE%` grants access only to the current user, SYSTEM, and the Administrators group
- **THEN** telemetry configuration loads it without a permission error
- **AND** the same file is refused when its access control list also grants read access to the Users group

### Requirement: Filesystem boundaries on Windows

On Windows, every operation Agent Marketplace performs below an authorized root (materialization, ownership and backup publication, configuration and lock publication, and state and lock files) SHALL stay bound to the directory it captured, so that renaming or replacing a path after capture cannot redirect the operation. Symbolic links, directory junctions, mount points, and every other reparse point at a path component below the root or at an output leaf SHALL be treated as unsafe links and MUST NOT be followed. The operation MUST be refused with the diagnostic code that the same condition produces on other platforms, and the link and everything it targets MUST be preserved. A regular file with more than one hard link MUST be refused as an output leaf. Path components that Windows interprets specially MUST be refused: device names such as `NUL`, `CON`, and `COM1`, alternate data stream syntax (`name:stream`), and names ending in a dot or a space. Refusal MUST NOT leave staging residue. Symbolic-link aliases of harness roots and of user configuration files, which other platforms honor under their link policies, are not honored on Windows and MUST be refused as unsafe links.

#### Scenario: Directory junction below a harness root is refused

- **WHEN** sync runs on Windows and an output directory below a harness target root is a directory junction to another directory
- **THEN** sync reports `materialize.destination.error` naming the junction
- **AND** the junction and the contents of its target are unchanged
- **AND** no staging file remains in either directory

#### Scenario: Hard-linked output is refused

- **WHEN** sync runs on Windows and a managed output file has a second hard link elsewhere
- **THEN** sync does not replace or modify that file
- **AND** both names keep their original bytes

#### Scenario: Windows-special names are refused

- **WHEN** sync runs on Windows for a skill whose supporting file is named `nul.txt`, `report:stream`, or `notes.`
- **THEN** sync reports `materialize.write.error` naming that file
- **AND** the skill's other files are not left partially materialized

#### Scenario: Symlinked user configuration is refused

- **WHEN** `install --scope user` runs on Windows and `%USERPROFILE%\.agents\config.yaml` is a symbolic link
- **THEN** the command exits non-zero without following the link
- **AND** the link and its target are unchanged

### Requirement: Concurrent mutations are serialized on Windows

On Windows, ownership mutations of a shared aggregate file and dependency transactions SHALL be serialized across processes with the same guarantees as on Unix. A state root on a local NTFS volume SHALL NOT produce `ownership.lock-unavailable`. A process that exits or is terminated while holding a lock SHALL release it without stale-lock recovery.

#### Scenario: Concurrent installs into one repository keep both selections

- **WHEN** two agent-marketplace processes on Windows run `install --scope repo` at the same time in one repository, one for MCP server `alpha` and one for MCP server `beta`, both targeting Codex
- **THEN** both commands exit 0
- **AND** `.agents/config.yaml` selects both servers and `.codex/config.toml` contains both as owned entries alongside every handwritten entry
- **AND** neither run reports `ownership.lock-unavailable`

#### Scenario: A terminated holder releases its lock

- **WHEN** an agent-marketplace process on Windows is terminated while holding an ownership lock
- **THEN** a subsequent sync of the same root acquires the lock without waiting for a timeout or reporting a stale lock

### Requirement: Materialization mode on Windows

On Windows, when no configuration layer sets a materialization mode for a harness target, its materialization mode SHALL be file. An explicitly configured symlink materialization mode SHALL be honored. When the operating system refuses to create a symbolic link for a harness target in symlink materialization mode, sync SHALL report `materialize.symlink.unavailable` for that harness target and SHALL NOT write, replace, or prune any of its outputs; other harness targets SHALL proceed and sync SHALL exit non-zero. The message MUST say that Windows requires Developer Mode or administrator rights to create symbolic links, and MUST name `materialization.mode: file` as the alternative. `sync --check` MUST NOT create links to probe this capability.

#### Scenario: Default materialization mode on Windows is file

- **WHEN** sync runs on Windows and no configuration layer sets a materialization mode
- **THEN** selected skills are written as regular directory trees
- **AND** `sync --check` reports every harness target in sync

#### Scenario: Symlink refusal is reported without partial output

- **WHEN** sync runs on Windows for a harness target explicitly configured for symlink materialization mode and symbolic link creation is refused
- **THEN** sync exits non-zero and reports `materialize.symlink.unavailable` naming that harness target and `materialization.mode: file`
- **AND** no output of that harness target is written, replaced, or pruned
- **AND** outputs of other selected harness targets are materialized

### Requirement: Registry content is acquired without line-ending conversion

Agent Marketplace SHALL acquire registry content on every platform without converting line endings based on the host platform or the user's Git configuration. Line-ending attributes declared explicitly by the registry repository SHALL apply identically on every platform. For the same registry commit, exact snapshot verification, source revision manifests, and materialized bytes SHALL be identical on Windows, Linux, and macOS.

#### Scenario: Registry with text auto-detection verifies on Windows

- **WHEN** a registry repository declares `* text=auto` in `.gitattributes`, its acquisition lock was written on Linux, and `sync --locked` runs on Windows with Git configured with `core.autocrlf=true`
- **THEN** exact snapshot verification succeeds
- **AND** the materialized skill files are byte-identical to those materialized on Linux

### Requirement: Committed files are platform-independent

Acquisition locks SHALL be written with `\n` line endings and `/`-separated paths on every supported platform. For the same configuration and registry state, a repository acquisition lock written on Windows SHALL be byte-identical to one written on Linux or macOS. A lock that differs from the desired content only by `\r\n` line endings SHALL be treated as current: it SHALL pass `sync --locked`, and plain sync SHALL NOT rewrite it. Configuration files created on any platform SHALL use `\n` line endings and `/`-separated relative paths; a configuration edit SHALL preserve the line-ending style of the file it edits.

#### Scenario: Lock written on Windows matches Linux

- **WHEN** the same repository configuration is locked on Windows and on Linux against the same registry commit
- **THEN** the two `.agents/lock.yaml` files are byte-identical

#### Scenario: Lock checked out with CRLF line endings is current

- **WHEN** a repository acquisition lock committed from Linux is checked out on Windows with `\r\n` line endings and `sync --locked` and then plain `sync` run
- **THEN** both commands succeed
- **AND** `.agents/lock.yaml` is unchanged

#### Scenario: Configuration edit preserves CRLF line endings

- **WHEN** `install --scope repo` edits a `.agents/config.yaml` that uses `\r\n` line endings
- **THEN** every line of the edited file ends with `\r\n`
- **AND** a relative `directory:` path written by the edit uses `/` separators

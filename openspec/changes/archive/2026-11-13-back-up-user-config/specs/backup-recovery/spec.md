## ADDED Requirements

### Requirement: Harness backups include a user configuration archive
Whenever a sync writes harness backups, it SHALL also write `~/.agent-marketplace/backups/agents-config/<timestamp>.tar.gz` with permission 0600. The archive SHALL hold `config.yaml`, `lock.yaml`, and every local include fragment inside `~/.agents/`, named by their paths relative to `~/.agents/`. A missing file SHALL be left out without an error, and no archive SHALL be written when none of these files exist.

#### Scenario: Sync archives user configuration with harness backups
- **WHEN** a sync writes harness backups and `~/.agents/` holds `config.yaml`, `lock.yaml`, and an include fragment that `config.yaml` names
- **THEN** a new archive under `~/.agent-marketplace/backups/agents-config/` holds those three files at their paths relative to `~/.agents/` with their bytes, and the archive file has permission 0600

#### Scenario: Missing user configuration files are left out
- **WHEN** a sync writes harness backups and `~/.agents/lock.yaml` does not exist
- **THEN** the sync succeeds and the configuration archive holds `config.yaml` without `lock.yaml`

#### Scenario: No configuration archive without user configuration files
- **WHEN** a sync writes harness backups and none of `config.yaml`, `lock.yaml`, or an include fragment exists in `~/.agents/`
- **THEN** the sync succeeds and no archive is written under `~/.agent-marketplace/backups/agents-config/`

#### Scenario: Include fragment outside the user configuration directory is not archived
- **WHEN** `~/.agents/config.yaml` includes a fragment outside `~/.agents/` and a sync writes harness backups
- **THEN** the configuration archive does not contain that fragment and a debug log line names its path

### Requirement: User configuration archive holds command-start bytes
The configuration archive SHALL hold the bytes of `config.yaml`, `lock.yaml`, and include fragments as they were on disk when the command started, even when the same command later rewrites them.

#### Scenario: Configuration archive keeps the lock from before the sync
- **WHEN** a sync that writes harness backups also rewrites `~/.agents/lock.yaml`
- **THEN** the configuration archive's `lock.yaml` holds the bytes that were on disk before the sync started

### Requirement: On-demand backup includes the user configuration archive
`agent-marketplace backup create` SHALL write the configuration archive whenever it writes harness backups, and its `Backup created for:` line SHALL name `agents-config` when it writes one.

#### Scenario: Backup create names the configuration archive
- **WHEN** a user runs `agent-marketplace backup create` with managed harness paths and `~/.agents/config.yaml` present
- **THEN** an archive is written under `~/.agent-marketplace/backups/agents-config/` and the `Backup created for:` line names `agents-config`

### Requirement: Configuration archives follow backup retention and listing
Configuration archives SHALL be rotated by the configured `max_days` and `max_per_day` settings like harness archives, and `backup list` SHALL show them in their own `agents-config` group.

#### Scenario: Configuration archives are rotated by the per-day cap
- **WHEN** a backup run leaves more configuration archives from one day than `max_per_day`
- **THEN** the oldest configuration archives of that day are removed until `max_per_day` remain

#### Scenario: Backup list shows configuration archives in their own group
- **WHEN** a user runs `agent-marketplace backup list` after a configuration archive was written
- **THEN** the output lists that archive's timestamp under an `agents-config` group separate from harness target groups

### Requirement: Configuration archives are not restored by the restore command
`agent-marketplace backup restore --target agents-config` SHALL fail with an error that says configuration archives are restored by hand with `tar`, and SHALL change nothing on disk.

#### Scenario: Restore refuses the configuration archive target
- **WHEN** a user runs `agent-marketplace backup restore --target agents-config` while configuration archives exist
- **THEN** the command exits with an error naming manual `tar` extraction, and `~/.agents/` and the backups directory are unchanged

### Requirement: Configuration archive is skipped when backups are not written
No configuration archive SHALL be written when backups are disabled in configuration, when `sync --no-backup` is used, or by the read-only modes `sync --check`, `sync --locked`, and `lock --check`.

#### Scenario: Disabled backups write no configuration archive
- **WHEN** backups are disabled in configuration and a user runs `agent-marketplace sync`
- **THEN** no archive is written under `~/.agent-marketplace/backups/agents-config/`

#### Scenario: Sync without backup writes no configuration archive
- **WHEN** a user runs `agent-marketplace sync --no-backup`
- **THEN** no archive is written under `~/.agent-marketplace/backups/agents-config/`

#### Scenario: Read-only modes write no configuration archive
- **WHEN** a user runs `agent-marketplace sync --check`, `agent-marketplace sync --locked`, or `agent-marketplace lock --check`
- **THEN** no archive is written under `~/.agent-marketplace/backups/agents-config/`

### Requirement: Configuration archive failure stops sync before harness changes
If writing the configuration archive fails, the sync SHALL fail before changing any harness file, the same way a failed harness backup does.

#### Scenario: Configuration archive failure leaves harness files unchanged
- **WHEN** writing the configuration archive fails during a sync that would change harness files
- **THEN** the sync reports the backup error and every harness file keeps its prior bytes

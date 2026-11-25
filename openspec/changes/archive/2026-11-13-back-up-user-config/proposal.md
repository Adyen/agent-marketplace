# Proposal

## Why

Harness backups under `~/.agent-marketplace/backups/<target>/` archive only materialized harness output. The user-layer configuration that produced that output (`~/.agents/config.yaml`, its local include fragments, and `~/.agents/lock.yaml`) is not archived, so when those files are lost or reverted the only way back is to reverse-engineer them from harness archives. A configuration snapshot taken with each harness backup turns that recovery into a `tar` extract.

## What Changes

- Whenever a sync writes harness backups, it also writes `~/.agent-marketplace/backups/agents-config/<timestamp>.tar.gz` holding `config.yaml`, `lock.yaml`, and every local include fragment inside `~/.agents/`, with paths relative to `~/.agents/` and the bytes on disk when the command started. Missing files are left out; no archive is written when none exist.
- `agent-marketplace backup create` writes the same archive whenever it writes harness backups and names `agents-config` in its `Backup created for:` line.
- Configuration archives follow the existing `max_days`/`max_per_day` rotation and appear in their own `backup list` group. `backup restore --target agents-config` refuses with a clear error and changes nothing; restoring is a manual `tar` extract.
- No configuration archive is written when backups are disabled, with `sync --no-backup`, or by read-only modes (`sync --check`, `sync --locked`, `lock --check`).
- A failure writing the configuration archive stops the sync before any harness file changes.
- Include fragments outside `~/.agents/` are not archived and are named in a debug log line.
- Harness archive layout, rotation, and restore are unchanged. Repository-layer and organization files are not archived.

## Capabilities

### New Capabilities

### Modified Capabilities
- `backup-recovery`: adds requirements for the user configuration archive: its contents and command-start bytes, on-demand creation, rotation and listing, restore refusal, the modes that skip it, and failure ordering.

## Impact

`internal/backup` (configuration archive writer), `internal/pipeline` (sync backup step and user configuration capture), `cmd/agent-marketplace` (`sync` and `backup` commands), and the operator backup and filesystem-layout documentation. No configuration, lock, or harness archive format change.

## Release note

Each harness backup now also archives your user configuration (`~/.agents/config.yaml`, its local include fragments, and `~/.agents/lock.yaml`) to `~/.agent-marketplace/backups/agents-config/`, so a lost or reverted configuration can be restored with `tar`.

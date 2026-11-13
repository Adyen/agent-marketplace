# Design

## Context

Sync backs up harness output in its backup step, after resolution and before materialization: `backup.Create` writes one archive per harness target, `backup.CreateCodexSet` writes the Codex set, and `backup.Rotate` applies retention to every directory under the backups root. `backup list` already lists every such directory. The user configuration lock can be rewritten before that step (lock preparation in the `sync` command, and the exact-lock lease at the start of the pipeline sync), so reading the files at the backup step would archive bytes the sync itself produced.

## Goals / Non-Goals

**Goals:**
- Archive `~/.agents/config.yaml`, `~/.agents/lock.yaml`, and the local include fragments inside `~/.agents/` with each harness backup, as they were when the command started.
- Reuse the existing archive output, retention, and listing paths.

**Non-Goals:**
- `backup restore` support for configuration archives.
- Repository-layer files and the organization configuration or lock.
- Separate snapshots before `install`, `remove`, `update`, `lock`, or TUI configuration edits; they are covered only when they run a sync that backs up.
- Any change to the harness archive layout, rotation, or restore.

## Decisions

- **Capture in memory at command start, write at the backup step.** The `sync` and `backup create` commands read the files into memory before lock preparation or any write. The pipeline sync captures them itself at its start when its caller supplied no snapshot, so syncs started by other commands and the TUI are covered without changing them. A read error is kept with the snapshot and reported only when the archive would be written, so it fails the sync at the same point as a harness backup failure. Writing from memory, not re-reading at the backup step, is what keeps the lock's command-start bytes.
- **One archive directory, `agents-config`.** The archive lives beside the harness target directories, so `backup.Rotate` and `backup.ListBackups` handle it unchanged. Its archive is written through the existing exclusive 0600 archive output path. The name contains no harness target, so it never collides with a target directory.
- **Include discovery mirrors the loader.** `include:` paths are read from the captured bytes, resolved against the canonical directory of the including file, followed recursively with a cycle guard and the loader's file limit. A fragment whose canonical path is outside canonical `~/.agents/` is read for its own includes but not archived, and a debug log line names it. `config.yaml` and `lock.yaml` are archived by name; a symlink to either is followed. Only regular files are read, opened non-blocking.
- **Gating.** The archive is written only inside the harness backup step, which already runs only when backups are enabled, `--no-backup` is absent, and harness targets exist. The `sync` command passes no snapshot and disables the configuration archive in locked mode. `--check` and `--offline` imply locked mode there, so they write none. `lock --check` runs no backup step.
- **Restore refusal.** `backup restore --target agents-config` is answered before any filesystem access with an error naming the archive directory and a `tar -xzf <archive> -C ~/.agents` command.

## Risks / Trade-offs

- An unreadable user configuration file now fails a sync that writes backups. This matches how an unreadable harness file fails the harness backup, and `--no-backup` remains the escape.
- A first sync that finds no existing harness output writes a configuration archive even though no harness archive is written, because the backup step runs whenever harness targets exist.

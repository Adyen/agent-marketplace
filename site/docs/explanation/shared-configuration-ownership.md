# Shared configuration ownership

Some harnesses keep hooks and MCP servers in one shared file instead of one file per resource. Devin uses `mcp_config.json`, Codex uses `.codex/hooks.json` and `.codex/config.toml`, and Gemini keeps MCP servers in `.gemini/settings.json`. You may have written entries in those files yourself. `agent-marketplace` adds and updates its own entries there, and it must never delete or overwrite yours. To tell the two apart, it keeps a record of the entries it wrote. The docs call that record [ownership state](../glossary.md#ownership-state).

This page covers Devin, Codex, and Gemini MCP servers. Other harnesses mark managed entries inside the file instead; [native harness materialization](./native-harness-materialization.md#shared-configuration-files) explains the difference.

## Why ownership is recorded outside the files

A marker such as `"_managed_by": "agent-marketplace"` inside a file proves nothing: any tool or person can copy it into an entry. If the CLI trusted it, a handwritten entry that happened to carry the marker could be deleted. So for these harnesses the CLI writes no marker. It stores ownership in its state folder, `$XDG_STATE_HOME/agent-marketplace/` (by default `~/.local/state/agent-marketplace/`). That folder is outside your projects and outside the cache, so clearing the cache has no effect on ownership.

## What the state records

The state is kept separately for each harness folder, such as a project's `.devin/` or your `~/.codex/`. For each shared file in that folder, it records which entries the CLI wrote and a fingerprint (digest) of each entry's value as written. Each change also increases a generation number, which lets the CLI detect two processes writing at once. The record holds names, digests, and generation numbers, not the commands, URLs, or secrets inside your entries.

Before each change to one of these files, the CLI saves a copy of the file as it was. It does this even when you have turned configured backups off, and keeps the five newest copies of each file.

## When the state is missing or damaged

If the ownership state is missing or cannot be read, the CLI has no proof that it wrote anything. It then treats every existing entry as yours, and during `sync` it:

- keeps every existing entry,
- refuses to write an entry whose name is already taken, and
- removes nothing.

The worst result is a stale entry you delete by hand. A handwritten entry is never lost. A problem in one file, such as invalid JSON in `mcp_config.json`, blocks only that file; the other files are still updated.

## Repairing ownership

`agent-marketplace doctor --repair` shows what it can fix in the Devin and Codex files and, when run in a [terminal](../glossary.md#terminal), asks before each change. It recognizes three situations:

- An existing entry is exactly what the CLI would write. Recording it as managed changes no bytes in the file, so `--yes` can do this without asking.
- An entry the CLI wrote earlier is no longer selected, and a saved backup or a recorded change proves the CLI wrote it. The CLI can remove it.
- An entry you did not install through the CLI has the same name as a selected resource but a different value. The CLI can replace it with the selected version.

Removing or replacing an entry deletes content, so the CLI asks you about each entry separately and saves a fresh backup first, even with `--yes`. Without a terminal, `--repair` only previews, unless `--yes` lets it adopt exact matches. [How to repair owned shared configuration](../how-to/repair-owned-shared-configuration.md) walks through a repair.

## Two processes at once

Two `sync` runs, in two terminals or in a script, could try to edit the same file at the same moment. Every change to one harness folder's ownership state takes an advisory file lock (`flock(2)`) for that folder, so only one process edits it at a time. The operating system releases the lock when the process exits, even after a crash, so a leftover lock cannot block later runs. On a file system that does not support these locks, the CLI falls back to comparing generation numbers: if another process changed the state in the meantime, the change is refused with `ownership.generation-conflict`, and nothing is overwritten. The CLI reports that code only when it detects such a conflict, not merely because locking is unavailable.

## Related documentation

- [How to repair owned shared configuration](../how-to/repair-owned-shared-configuration.md)
- [Native harness materialization](./native-harness-materialization.md)

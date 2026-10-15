# How to repair owned shared configuration

Fix Devin or Codex shared configuration files when `agent-marketplace` cannot prove that it wrote an entry that is already there.

Several sources can write entries into the same Devin or Codex file, such as `mcp_config.json` or `.codex/config.toml`. `agent-marketplace` records the entries it wrote as [ownership state](../glossary.md#ownership-state), and it changes or removes only those. When an entry it wants to write already exists without that record, `sync` leaves the entry alone and reports it: a warning for Devin files, an error for Codex files. `doctor --repair` resolves those cases one by one.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A `sync` run that reported an `ownership.*` diagnostic, such as `ownership.state-missing` or `ownership.unmanaged-key`.

`materialize.codex.trust-review` is a different message. It reminds you to review the repository's trust setting in Codex after its hooks or MCP configuration changed. It is not an ownership problem, and `--repair` does nothing for it.

## 1. Preview the repairs

```bash
agent-marketplace doctor --repair
```

After the normal `doctor` report, the command lists each Devin and Codex shared file it inspects, even files with nothing to repair. For each entry it can act on, it shows one of three actions:

- `adopt`: the existing entry is identical to what `agent-marketplace` would write. Adopting records it as owned and does not change the file.
- `replace`: the existing entry has the same key but a different value. Replacing overwrites your value with the generated one.
- `orphan candidate`: evidence shows that an earlier run of `agent-marketplace` wrote the entry, and nothing selects it anymore. Removing it deletes the entry.

When there is nothing to repair, the preview ends like this:

```text
repair: Codex shared-aggregate ownership
repair: devin shared-aggregate ownership
  nothing to repair
```

If standard input is not a terminal (for example in a script), the command stops after the preview. It prints `preview only: not a terminal; re-run on a terminal to confirm, or use --repair --yes to adopt exact matches` when there is something to confirm. `--yes` is described in the next step.

## 2. Confirm each repair

On a terminal, the command asks about each entry, for example `adopt mcp-server "docs" in mcp_config.json? [y/N]`. Only `y` or `yes` confirms. Any other answer, or pressing Enter, leaves the entry unchanged.

To adopt every exact match without being asked:

```bash
agent-marketplace doctor --repair --yes
```

`--yes` covers adoption only. Each adopted file is reported as `adopted <n> entr(y/ies) in <file>`. Replacing a conflicting entry or removing an orphan still asks you on a terminal, and is skipped when there is no terminal. Each skipped entry is printed as `left unchanged`.

## 3. Verify

```bash
agent-marketplace sync --check
```

The check exits with status 0 when the shared configuration matches your selection.

## What `--repair` guarantees

- It never removes or overwrites an entry without your confirmation on a terminal.
- It takes a fresh safety backup of the file before it replaces or removes an entry.
- It never edits your harness lists.
- It exits with status 1 if the ownership state or a shared file is corrupt, or if a confirmed repair fails. Declining every prompt is not a failure.
- When the ownership state is missing, every existing entry counts as unmanaged. `sync` then keeps all of them, refuses to write over any of them, and removes nothing.

## Related documentation

- [Native harness materialization](../explanation/native-harness-materialization.md)
- [Shared configuration ownership](../explanation/shared-configuration-ownership.md)

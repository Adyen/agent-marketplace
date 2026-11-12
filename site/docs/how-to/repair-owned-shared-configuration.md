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

After the normal `doctor` report, the command prints a numbered repair plan. Each line shows the step's safety class (`safe`, `destructive`, or `human-only`), the action, the diagnostic code it addresses, the files it touches, and either the inputs it needs or `blocked by N (<reason>)` naming the plan item that blocks it. It then lists each Devin and Codex shared file it inspects, even files with nothing to repair. For each entry it can act on, it shows one of three actions:

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

## 2. Confirm the repairs

On a terminal, the command asks one `[y/N]` confirmation for all the safe steps in the plan, for example `Repair the safe steps above? [y/N] `. Only `y` or `yes` confirms. Any other answer, pressing Enter, or end of input runs no step, exits 0, and leaves every file unchanged. Adoption is a safe step, so one answer adopts every exact match.

Replacing a conflicting entry and removing an orphan are destructive: each entry keeps its own per-entry confirmation on a terminal, and the command takes a fresh backup first. Destructive steps never run under `--yes` or off a terminal. On a terminal under `--yes` the per-entry prompt is still shown, but a confirmed destructive entry is reported and left unchanged.

To run the safe steps without the plan confirmation:

```bash
agent-marketplace doctor --repair --yes
```

Each adopted file is reported as `adopted <n> entr(y/ies) in <file>`; every entry a destructive step would change stays byte-identical and is printed as `left unchanged`.

After each step runs, the command diagnoses the setup again and shows any new steps for confirmation before they run. A step that keeps recurring stops the repair after five runs with exit status 1.

## 3. Verify

```bash
agent-marketplace sync --check
```

The check exits with status 0 when the shared configuration matches your selection.

## What `--repair` guarantees

- It never removes or overwrites an entry without your confirmation on a terminal.
- It takes a fresh safety backup of the file before it replaces or removes an entry.
- It never edits your harness lists.
- An off-terminal preview or a `--yes` run can exit 0 with destructive entries deferred. Otherwise, status 0 means the setup ended clean or you declined. Status 1 means a confirmed step failed, corrupt ownership state or a corrupt shared file remains, or the plan is left with only blocked or `human-only` items. An invalid configuration or a failed resolution is reported as a plan item and blocks the steps that need it rather than refusing the whole repair.
- When the ownership state is missing, every existing entry counts as unmanaged. `sync` then keeps all of them, refuses to write over any of them, and removes nothing.

## Related documentation

- [Native harness materialization](../explanation/native-harness-materialization.md)
- [Shared configuration ownership](../explanation/shared-configuration-ownership.md)

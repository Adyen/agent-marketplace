# How to convert a harness layout

Convert a folder of resources written for one [harness](../glossary.md#harness) into the layout another harness expects, for inspection or migration.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A source folder in a supported harness layout, for example a Claude Code plugin.
- An output folder, ideally new or empty. Keep it separate from the source: the command refuses an output folder that is the source, is inside it, or contains it.

## 1. Preview the conversion

```bash
agent-marketplace convert \
  --from claude \
  --to factory \
  --out /tmp/factory-output \
  --dry-run \
  ./claude-plugin
```

`--from` names the source layout and `--to` names the target. If you do not know the source layout, replace `--from` with `--autodetect`. Without `--out`, the output goes to a folder named `<path>-<to>` next to the source, `./claude-plugin-factory` in this example.

`--dry-run` reports the planned writes without touching disk. The summary looks like this:

```text
Converted claude -> factory into /tmp/factory-output
Written 1, Skipped 0, Removed 0, Diagnostics 0
```

`Written` counts files to create or change, `Skipped` counts files that already have identical content, `Removed` counts files to delete, and `Diagnostics` counts conversion messages.

## 2. Run the conversion

Run the same command without `--dry-run`:

```bash
agent-marketplace convert \
  --from claude \
  --to factory \
  --out /tmp/factory-output \
  ./claude-plugin
```

By default the command replaces files at the paths it writes and leaves every other file in the output folder alone.

::: warning `--sync` deletes other files in the output folder
With `--sync`, the command also deletes every file in the output folder that this conversion did not produce, including files you added by hand. Use it only on a folder that holds nothing but conversion output, and preview with `--sync --dry-run` first: the `Removed` count shows how many files would be deleted. When a resource fails to convert, the command skips the deletions.
:::

## 3. Inspect the result

```bash
ls /tmp/factory-output
```

The folder contains the resources in the Factory Droid layout.

If some resources could not be converted, the command lists each one as `skipped <kind> "<name>" (<codes>)` and exits with an error, while the resources that did convert are written.

## Supported directions

Every supported harness can be both a source and a target: `claude`, `factory`, `cursor`, `windsurf`, `gemini`, `devin`, and `codex`. Each run has one source and one target.

## Codex conversion restrictions

When converting to Codex, the command refuses an output folder that already contains `.codex/hooks.json` or `.codex/config.toml`. Those files can hold entries from several sources, and a one-shot conversion does not merge into them. Use a new or empty folder for Codex output.

## Troubleshooting

- A field is missing after conversion: some fields exist only in the source harness and have no equivalent in the target. The `Diagnostics` count and the [diagnostics reference](../reference/diagnostics.md) explain what was dropped.
- `codex shared file ... already exists; in-place conversion to Codex is unsupported; choose an empty destination`: pick a new `--out` folder.
- `conversion source and output paths overlap`: choose an output folder outside the source folder.
- `autodetect: could not identify source harness`: pass `--from` with the source layout.

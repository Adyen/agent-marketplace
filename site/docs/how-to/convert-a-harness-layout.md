# How to convert a harness layout

Convert a resource from one harness layout to another for inspection or migration.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A source directory in a supported harness layout.

## 1. Convert from a source to a destination

Conversion is bidirectional across all supported harnesses. Each invocation has one source and one destination. By default the command writes in overwrite mode: existing files at the same paths are replaced and other files in `--out` are left alone. Add `--sync` to also prune files that a previous conversion wrote but that this run no longer produces, and `--dry-run` to see the planned writes without touching disk. When `--out` is omitted, the output goes to `<path>-<to>`.

Convert a Claude Code plugin to Factory Droid format:

```bash
agent-marketplace convert \
  --from claude \
  --to factory \
  --out /tmp/factory-output \
  ./claude-plugin
```

## 2. Inspect the result

```bash
ls /tmp/factory-output
```

The output contains Factory Droid native files.

## Supported directions

Every supported harness can be both a source and a destination: `claude`, `factory`, `cursor`, `windsurf`, `gemini`, `devin`, and `codex`.

## Codex conversion restrictions

Converting **to** Codex is the one case with a destination precondition: the command refuses an `--out` directory that already contains `.codex/hooks.json` or `.codex/config.toml`, because those are shared, ownership-reconciled files and one-shot conversion does not merge into them. Use a new or empty directory for Codex output.

## Troubleshooting

- **Conversion drops fields**: Some fields are native-only and have no equivalent in the target. Check the [diagnostics reference](../reference/diagnostics.md) for the emitted warning codes.
- **`codex shared file ... already exists`**: You converted to Codex into a directory that already has `.codex/hooks.json` or `.codex/config.toml`. Pick a fresh `--out`.

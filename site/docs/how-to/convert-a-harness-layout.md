# How to convert a harness layout

Convert a resource from one harness layout to another for inspection or migration.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A source directory in a supported harness layout.

## 1. Convert from a source to a destination

Conversion is bidirectional across all supported harnesses. Each invocation has a single source and a single destination. The destination directory must be empty or non-existent, because the command writes the full converted tree.

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

Converting to Codex requires a destination without pre-existing `.codex/hooks.json` or `.codex/config.toml`. Use a new or empty directory.

## Troubleshooting

- **Conversion drops fields**: Some fields are native-only and have no equivalent in the target. Check the [diagnostics reference](../reference/diagnostics.md) for the emitted warning codes.
- **Destination already exists**: Use a fresh `--out` directory rather than manually cleaning target files.

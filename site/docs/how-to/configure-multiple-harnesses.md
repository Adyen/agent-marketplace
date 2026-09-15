# How to configure multiple harnesses

This guide shows how to target more than one coding harness at the same time.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A personal or repository configuration file.

## 1. Select targets in the TUI

Launch the TUI:

```bash
agent-marketplace
```

Press `H` to open the harness modal. Toggle each target with `space` and press `enter`. The selection is saved to the active configuration file.

## 2. Or edit the configuration directly

Add the harnesses to `~/.agents/config.yaml` or `.agents/config.yaml`:

```yaml
apiVersion: agent-marketplace/v2
harnesses:
  - claude
  - cursor
  - devin
```

## 3. Narrow a registry to specific harnesses

A registry can emit only a subset of the global targets:

```yaml
registries:
  - url: https://github.com/example-org/claude-tools.git
    ref: v1.0.0
    harnesses:
      - claude
```

The entry's `harnesses` list intersects with the effective list.

## 4. Sync and verify

```bash
agent-marketplace sync
```

Check the output:

```text
Installed 3  Removed 0  Malformed 0  Errors 0
```

Each selected resource now appears under the directories for every applicable target harness.

## Troubleshooting

- **Unknown harness error**: Check the identifier against the [harnesses and output paths reference](../reference/harnesses-and-output-paths.md).
- **A registry entry does not appear for one harness**: Verify the entry's `harnesses` list includes that target.

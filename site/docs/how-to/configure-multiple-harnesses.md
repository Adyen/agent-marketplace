# How to configure multiple harnesses

Install your resources into more than one [harness](../glossary.md#harness) at the same time, for example Claude Code and Cursor.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- A personal or repository configuration file.

## 1. Select targets in the TUI

Launch the TUI:

```bash
agent-marketplace
```

Press `H` to open the harness modal. It opens on your personal list in `~/.agents/config.yaml`. Press `p` to edit the repository list in `.agents/config.yaml` instead, or `g` to go back. Use the arrow keys to move, `space` to toggle a harness, and `enter` to save. `esc` closes the modal without saving.

## 2. Or edit the configuration directly

Add the harnesses to `~/.agents/config.yaml` or `.agents/config.yaml`:

```yaml
apiVersion: agent-marketplace/v4
harnesses:
  - claude
  - cursor
  - devin
```

The lists do not merge. Inside a repository whose `.agents/config.yaml` sets `harnesses`, sync uses that list for the whole run. Otherwise it uses your personal list, and then the organization's.

Each file's resources also keep that file's own `harnesses` list as a limit. Resources from your personal configuration go only to harnesses in both your list and the repository's list. If the two lists do not overlap, a sync inside that repository leaves out your personal resources and removes their machine-wide files until you sync somewhere else.

## 3. Narrow a registry to specific harnesses

A resource group can write to only some of the configured harnesses:

```yaml
sources:
  claude-tools:
    git: https://github.com/example-org/claude-tools.git
    ref: v1.0.0
resources:
  claude-tools:
    include: [skill/code-review]
    harnesses: [claude]
```

Sync writes this group only to harnesses that appear in both lists. With the configuration from step 2, `code-review` goes to Claude Code and nowhere else.

## 4. Sync and verify

```bash
agent-marketplace sync
```

The summary line starts with `Installed <n>  Removed <n>  Malformed <n>  Errors <n>`. When sync writes or removes output, it adds one `→` line. That line lists the changed output roots, separated by commas, each with its own counts in parentheses, for example `→ /path/.claude/skills (1 written), /path/.cursor/skills (1 written)`. `n` counts the files and entries written across all harnesses. It depends on your selection and harnesses, not on the number of resources.

Check that each selected resource appears in every harness it applies to:

```bash
agent-marketplace status
```

The `HARNESS` column under `SYNCED RESOURCES` lists every harness a resource goes to, for example `claude,cursor`, and `HARNESS TARGETS` shows each harness's folders.

## Troubleshooting

- If sync reports an unknown harness, check the identifier against the [harnesses and output paths reference](../reference/harnesses-and-output-paths.md).
- If a registry's resources do not appear for one harness, check that the resource group's `harnesses` list includes it.

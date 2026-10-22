## Why

In symlink mode, the default, a Gemini subagent was never removed once it was the last one selected (#919). After `remove` and `sync`, and after `deinit`, `.gemini/agents/<name>.md` stayed in place and `sync --check` reported no drift. File mode already removed it. The Gemini compiler has written `.gemini/agents` since the import commit 6414a3e6, but the native layout never listed `agents` as a component, an omission rather than a decision. Symlink-mode cleanup is planned for declared components and for directories that still receive output, so `agents` was cleaned only while another Gemini subagent was still selected, and the machine-scope safety backup never archived it.

## What Changes

- The Gemini native layout declares `agents` as a component, as the layouts of the other harnesses with subagents do.
- `sync` removes a Gemini subagent that is no longer selected, including the last one of a scope, in both scopes and both materialization modes. `deinit` removes it with the rest of the scope's output. Handwritten files in `agents/` stay.
- Machine-scope safety backups archive `~/.gemini/agents`.
- Existing installs keep their links. The first `sync` after an upgrade reports no drift for a selected subagent and removes a link that an earlier version left behind.
- A `.gemini/agents` symlink to an existing directory is now refused like the other declared component directories, even when no Gemini subagent is selected.

## Capabilities

### New Capabilities

### Modified Capabilities
- `materialization`: deselected Gemini subagents are removed by `sync` and `deinit`, and machine-scope backups archive them.

## Impact

The `pkg/harness-compilation/target` native layout, read through `internal/harness` by `internal/materialize` and `internal/pipeline`. How Gemini subagents install, their link targets and blocked-registry removal are unchanged.

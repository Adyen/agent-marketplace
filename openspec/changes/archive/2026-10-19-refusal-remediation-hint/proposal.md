## Why

A consumer format refusal (`v4.api-version-required` or `v4.api-version`) says what is wrong but not what to do next. Users who hit it at CLI or TUI startup have to find the conversion guide and the right older migrator on their own, and the TUI's generic config recovery copy ("fix the file and field named above") invites the unsupported shortcut of editing only `apiVersion`.

## What Changes

- Version refusals for user-personal, repository-committed, and personal-local roots and their include trees carry a remediation hint: the older released migrator (agent-marketplace v4.1.4), the layer's `migrate --to v4 --scope <layer> --dry-run` preview command for that older binary, a link to the "Convert configuration before upgrading" guide, and a warning that conversion can refuse.
- Version refusals for the organization root and its include tree carry an administrator variant with the same guide link and no migrate command.
- CLI command errors and the TUI diagnostics view show the hint once. Other diagnostics keep their current guidance.
- The shipped CLI still never converts, prompts, or acquires a converter.

## Capabilities

### New Capabilities

### Modified Capabilities
- `consumer-config-format`: version refusals name a concrete next step.

## Impact

Consumer configuration validation diagnostics, CLI command error output, the TUI diagnostics view, and the conversion and diagnostics reference docs. No configuration, lock, or exit-code behavior changes.

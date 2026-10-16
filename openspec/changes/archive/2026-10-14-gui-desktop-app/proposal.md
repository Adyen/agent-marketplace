## Why

People who do not use a terminal need a local desktop client with the same core resource-management behavior as the TUI. A user-scope-only GUI with guided initialization and explicit configuration boundaries makes those operations accessible without introducing a second configuration editor.

## What Changes

- Add a desktop workspace, first-run setup with init-equivalent Harness target and Community telemetry consent choices, and an optional source step using the user configuration form.
- Provide resource browsing, detail inspection, install/remove, sync, update, status/drift, refresh, and operation feedback.
- Restrict the GUI to the user-personal root and read-only organization layer; compare modeled edits with equivalent CLI mutation bytes and identify unmodeled values as “managed outside the GUI”.
- Add keyboard-operable, accessibly named controls and GUI-surface telemetry under the existing consent and organization policy.
- Enforce interactive-surface test parity for every `session-*` scenario; this layer binds every such scenario from GUI tests.
- Add an organization-only `gui.version_check` policy with a default-on public-release check and lower-layer warning behavior; ship CLI read support before administrators are told to use the key.

## Capabilities

### New Capabilities
- `gui-shell`: User-scope workspace, single-instance behavior, reload, accessibility, version notice, telemetry, and absence of raw configuration editing.
- `gui-first-run`: Guided setup for an absent user-personal root with detected Harness targets, init-equivalent telemetry consent, and optional sources excluded from the init byte comparison.
- `gui-configuration`: User-personal Harness target and source forms, read-only organization values, allowlist enforcement, CLI mutation parity, and “managed outside the GUI” indication for unmodeled values.

### Modified Capabilities
- `config-layering`: Add the organization-only `gui.version_check` setting, default-on behavior, and warning/ignore handling for lower-layer values.
- `spec-governance`: Require each `session-*` scenario to have both TUI and GUI test witnesses and report the missing surface.

## Impact

The desktop host and frontend, interactive-session core and local API, user configuration and initialization paths, strict CLI configuration readers, admin documentation/release notes, telemetry attributes, and `tools/specaudit` governance checks are affected. This is the application layer after `gui-local-api`; rollout compatibility and CLI-equivalence details are in design.md.

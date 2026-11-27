## Why

Disabling an optional organization default currently rejects `[g]` with a toast and forces the user into a separate `[e] user override` flow, while disabling a project-committed resource locally requires a separate `[e] local exclude` flow instead of `[l]`. Users perceive "user exception" and "local exclude" as heavy-handed internal jargon for simply turning a resource off globally or locally.

## What Changes

- Unify optional organization-default Marketplace toggling under `[g]`: pressing `[g]` on an enabled optional organization default writes a user exclusion transparently (labeled `[g] disable for me (overrides org default)`), and pressing `[g]` on a user-excluded optional organization default clears that exclusion (`[g] restore org default`).
- Unify repo-local disabling of project-committed resources under `[l]`: pressing `[l]` on a non-required, non-machine-scope resource enabled in project config that supports personal-local exclusion writes a personal-local exclude transparently (labeled `[l] disable locally (overrides project)`), and pressing `[l]` when locally excluded clears that exclude (`[l] restore project default`). Project Marketplace pins keep `[l]` as an installation-scope control.
- Simplify the toggle modal options so users no longer see a separate `[e]` exclude row; keep required resources protected and machine-scope local-exclude refusal semantics intact.

## Capabilities

### Modified Capabilities

- `tui-interactions`: unified `[g]` and `[l]` toggle semantics for optional organization defaults and repo-local exclusions.

## Impact

`internal/tui/modal.go`, `internal/tui/forms.go`, `internal/tui/bulk.go`, `internal/tui/keys.go`, toggle modal unit tests, `site/docs/reference/tui.md`, and `tools/tui-qa/tests/`. No config file schema or resolver semantics change.

## Release note

Unified TUI toggle shortcuts: `[g]` now disables or restores optional organization defaults directly, and `[l]` disables or restores project resources locally without a separate `[e]` exception flow.

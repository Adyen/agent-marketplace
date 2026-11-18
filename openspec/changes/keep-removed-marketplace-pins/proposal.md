## Why

When a Marketplace resource is removed or renamed upstream, its latest-revision lookup returns 404, but the exact revision the lock pins usually stays downloadable. `update` currently keeps that pin and exits 1 on every run. A user who wants to keep the resource can never get a clean `update`: there is no way to acknowledge the removal, and targeted update only accepts Git registries. Scripts and CI that run `update` stay red until the resource is removed. `status` ignores the 404 entirely and reports the pin as `current`, and the TUI update shows `Update complete` without mentioning the removal.

## What Changes

- **BREAKING (exit status):** when a pin's latest-revision lookup returns 404 and its pinned revision is still usable (served by the Marketplace or present in the verified cache), `update` keeps the pin, prints a warning naming the resource and the `agent-marketplace remove <installed name>` remedy, and exits 0. The warning repeats on every `update` while the pin remains.
- A required pin removed upstream gets the same warning, with a hint naming the configuration entry that marks it required instead of `remove`.
- A removed resource whose pinned revision is no longer usable remains an error (exit 1), and other pins still update.
- `status` reports a pin whose latest lookup returns 404 with a `removed from the Marketplace; pinned revision kept` detail and a warning. Its STATE and exit status are unchanged.
- A TUI-started update reports the same warning in its completion toast count and diagnostics view instead of a bare `Update complete`.
- The "Handle renamed or removed resources" how-to describes the new behavior.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `update-output`: a removed resource with a usable pinned revision is a repeated warning, not an error.
- `status-reporting`: status notes pins removed from the Marketplace.
- `tui-error-recovery`: TUI update surfaces kept removed resources as warnings.

## Impact

Marketplace update planning and reporting (`internal/pipeline`, `internal/marketplace`, `cmd/agent-marketplace`), Marketplace status inspection (`internal/marketplace/status.go`), TUI update warning handling (`internal/tui`), a TUI QA journey, and `site/docs/how-to/handle-renamed-or-removed-resources.md`. No configuration, lock, or Marketplace API format change. Git registry update refusal is unchanged. Revoked revisions remain errors.

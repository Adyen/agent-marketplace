## Why

The TUI can toggle resources from configured sources and install Marketplace
discovery rows, but installing from a Git, web, or directory source that is not
configured yet still requires leaving the TUI for `install --from`. Users also
cannot see what a source contains before it is written to configuration.

## What Changes

- Add an install-from-source modal opened with `i`: enter a source URL, ref,
  and path; preview the resources it contains without writing anything; pick
  resources; choose user, project, or local scope; confirm.
- The preview reports the commit it resolved to. The install refuses, without
  publishing anything, when the source no longer resolves to that commit.
- Allowlist and registry-path checks reject a source before any network access.
- Malformed resources and empty sources cannot be installed; a source that
  expands into several bundles asks for one bundle path.
- Explicit-source installs accept kind-qualified picks, so resources of
  different kinds that share a name are unambiguous.
- `list --from` keeps its output; it shares the preview implementation.
- Document the `i` binding in the TUI reference and the `?` overlay.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-interactions`: adds installing selected resources from an unconfigured
  source through a preview, pick, and review flow.
- `resource-installation`: adds explicit-source installs bound to an expected
  commit and kind-qualified picks.

## Impact

- `internal/tui`: new modal, key binding, pane routing, help and footer text.
- `internal/pipeline`: shared source preview, install options for expected
  commit and kind-qualified picks.
- `cmd/agent-marketplace`: `list --from` uses the shared preview; dependency
  preparation exposes the commit it resolved.
- `tools/tui-qa`: new journey and fixture option.
- Docs: `site/docs/reference/tui.md`, `docs/tui-product-contract.md`.

## Release note

Press `i` in the TUI to install resources from a Git, web, or directory source that is not configured yet. You can preview what the source contains without writing anything, pick resources, and choose user, project, or local scope. The install refuses without writing anything if the source moved since the preview.

## Why

Collections appear in the TUI as opaque entries: users cannot tell what a collection represents, which skills or MCP servers it contains before installing it, or whether they can install individual items one by one. In `View → Kind`, collections sit in their own section without showing the MCP servers or skills they contain.

## What Changes

- Show each collection's direct members and included sub-collections (with kind and name) in the TUI detail pane and full detail view for both Git/local collections (`ir.Collection`) and Marketplace collections (loading verified collection manifests read-only via the existing Marketplace manifest endpoint without mutating config or locks).
- Render collections as expandable parent rows (`▼`/`▶`) in the resource list (reusing the `PluginParent` tree pattern in `internal/tui/tree.go`) so users can expand a collection inline to inspect its members, including under `View → Kind`.
- Allow users to select an individual member row under an expanded collection and install it directly as an independent resource pin (1-by-1 installation), while preserving collection-level toggle and removal semantics (coordinating with `agent-marketplace-t34y.2`).

## Capabilities

### Modified Capabilities

- `tui-navigation`: expandable collection rows and read-only Marketplace collection member inspection.
- `tui-interactions`: direct 1-by-1 installation of individual collection members from an expanded collection.
- `tui-rendering`: collection member summary in the detail pane.

## Impact

`internal/tui/tree.go`, `internal/tui/list.go`, `internal/tui/detail.go`, `internal/tui/marketplace_detail.go`, `internal/tui/modal.go`, `internal/session/marketplace_inspection.go` / `internal/session/catalog.go`, unit tests, and `tools/tui-qa/tests/`. Uses existing verified Marketplace collection manifest endpoints without breaking public API schemas.

## Release note

Collections in the TUI now show their contained resources in the detail pane, expand inline to browse members across all groupings including View by Kind, and allow installing individual members directly.

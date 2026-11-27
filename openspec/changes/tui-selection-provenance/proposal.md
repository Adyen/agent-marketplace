## Why

Optional organization-configured default resources currently render with the same `G` scope badge as user-installed global resources, and the detail pane does not explain which configuration layer enabled a resource or whether a collection pulled it in. Users cannot tell why a default skill is enabled or where it came from.

## What Changes

- Display `O` in the three-cell scope column for optional organization-layer selections (preserving `R` for required organization selections, `G` for user-personal, `P` for project, `L` for personal-local, their combinations, and `P-` for locally excluded project selections).
- Add a `Source layer:` field in the detail pane naming the enabling layer(s) in plain language (for example `organization default (optional)`, `organization (required)`, `user`, `project`, `local`).
- Add an `Included via:` field in the detail pane when a resource is selected through one or more collections (naming each collection).

## Capabilities

### Modified Capabilities

- `tui-rendering`: add `O` badge for optional organization defaults and display layer and collection provenance in the detail pane.

## Impact

`internal/tui/badges.go`, `internal/tui/detail.go`, `internal/session/rows.go` / `internal/session/catalog.go`, unit tests, and `tools/tui-qa/tests/`.

## Release note

Distinguished optional organization defaults from user-installed global resources with an `O` scope badge and added `Source layer:` and `Included via:` provenance fields to the TUI detail pane.

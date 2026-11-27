## MODIFIED Requirements

### Requirement: Scope-prefixed registry-qualified resource labels

Resource list rows SHALL display the existing status glyph, a three-cell letter scope column, the resource kind, and `registry/name`, in that order. The scope column SHALL retain `O` for optional organization defaults, `G`, `P`, `L`, their combinations, `P-`, and `R`, with a blank column for available or malformed rows. It SHALL NOT use word-based scope chips or omit scope information to fit a row. The registry prefix SHALL use the recorded display name, otherwise the registry URL's last segment without its `.git` suffix, otherwise `(local)`, and SHALL NOT append registry subpaths. The detail pane SHALL retain complete registry URL and path information. Selected and unselected rows SHALL use the same label content, keep long names readable at supported dimensions, preserve Unicode, escape hostile terminal data, and highlight name matches at the correct positions after the prefix.

#### Scenario: Aligned safe labels at normal and narrow sizes

- **WHEN** configured, optional-organization, multi-scope, locally excluded, required, available, and malformed rows render selected and unselected at normal and minimum supported sizes, including missing registry metadata, registry subpaths, long Unicode names, search matches, and hostile registry text
- **THEN** rows show the aligned letter scope (`O` for optional organization defaults, `G`/`P`/`L`/combinations, `P-`, `R`) before kind and registry-qualified name, blank scopes remain aligned, subpaths are omitted, hostile text appears as visible escapes, complete names remain readable, and highlights apply to matching name characters rather than the prefix

## ADDED Requirements

### Requirement: Selection layer and collection provenance in the detail pane

The TUI right-hand detail pane SHALL display a `Source layer:` field describing which configuration layer(s) selected the resource (`organization default (optional)`, `organization (required)`, `user`, `project`, or `local`), and SHALL display an `Included via:` field listing the collection name(s) whenever the resource was pulled in through a collection revision or Git collection.

#### Scenario: Optional organization default and collection-selected resource show provenance

- **WHEN** the user selects an optional organization default resource and then a resource pulled in by a collection
- **THEN** the optional organization resource MUST show badge `O` in the list and `Source layer: organization default (optional)` in the detail pane
- **AND** the collection-selected resource MUST show `Included via:` naming its parent collection in the detail pane

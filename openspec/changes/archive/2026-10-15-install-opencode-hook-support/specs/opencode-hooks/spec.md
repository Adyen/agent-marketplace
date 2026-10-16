## REMOVED Requirements

### Requirement: Hook contributions are additive and source-bound

**Reason**: Superseded by "Hook contributions and their support are additive and source-bound", which installs per-hook support instead of refusing support-dependent commands.

**Migration**: None. Hooks previously refused for plugin-root or project-directory references now install.

## ADDED Requirements

### Requirement: Hook contributions and their support are additive and source-bound

Supported hook contributions SHALL retain deterministic ordering, selection identity, and matcher grouping across registries, collections, scopes, and both materialization modes. Removing one selection SHALL retain contributions still selected by another scope or selector. Each selected hook's support files SHALL be the regular files under its source definition's directory, excluding the definition, native hook files of the source layout, and the top-level JSON files of an OpenCode declaration directory. They SHALL be installed with their verified bytes and permission bits under `hook-support/<source>/<hook>/` beneath the OpenCode root, as portable regular copies at repository scope in both materialization modes. Support SHALL never be installed into the declarations or plugin directories, and commands SHALL NOT point into the Agent Marketplace cache or a source directory. Unsafe, unreadable, replaced, or conflicting support input SHALL be refused with severity-error `materialize.hook.support-refused` or `materialize.hook.support-conflict`, preserving previously installed affected declarations, support, the adapter, and their ownership proof, while safely independent valid work proceeds.

#### Scenario: Removing one contribution preserves the others

- **WHEN** multiple sources contribute supported hooks and one selected contribution is removed
- **THEN** only that contribution's declaration and exclusively owned support SHALL be removed
- **AND** remaining contributions SHALL execute in the documented order alongside unrelated native plugins

#### Scenario: Hook support is installed beside the adapter

- **WHEN** a selected hook's command references a script beside its source definition through the plugin root
- **THEN** synchronization SHALL install the script under `hook-support/<source>/<hook>/` and the installed command SHALL run it from any working directory
- **AND** deselecting the hook SHALL remove its declaration and owned support

#### Scenario: Support refusal retains the installed hook generation

- **WHEN** a hook update encounters unsafe, unreadable, or replaced support input
- **THEN** synchronization SHALL report `materialize.hook.support-refused` and retain the installed declaration, support bytes, adapter, and ownership proof

#### Scenario: Sibling declarations are never installed

- **WHEN** a selected OpenCode hook's source directory also contains other hook declarations and support files
- **THEN** synchronization SHALL install only the selected declaration, its support files, and the fixed adapter
- **AND** no sibling declaration SHALL be copied or activated

### Requirement: Handlers receive the project directory

The fixed adapter SHALL set `AGENT_MARKETPLACE_PROJECT_DIR` for every handler. Its value SHALL be the directory containing the adapter's `.opencode` root, or the OpenCode instance directory for an adapter in any other configuration directory. Repository-scope plugin-root references SHALL resolve through it to the installed support directory. Machine-scope plugin-root references, and those of a repository selection relocated outside the repository, SHALL use the absolute installed support path. Project-directory references SHALL translate to it. A plugin-root reference without an installed support directory SHALL be refused with `hook.opencode.unsupported` and `hook.support-path-unrepresentable`.

#### Scenario: Support and project references follow the adapter project directory

- **WHEN** an installed repository-scope hook references its support or the project directory and OpenCode is started in the repository or one of its subdirectories
- **THEN** the handler SHALL reach the installed support and project paths through `AGENT_MARKETPLACE_PROJECT_DIR`
- **AND** a machine-scope hook SHALL reach its support through the absolute installed path

### Requirement: Declaration changes are reviewed

When synchronization adds, replaces, or removes an owned OpenCode hook declaration, it SHALL print the hook review listing with the harness target, scope, declaration file, event, matcher, and exact command before and after, and SHALL emit warning `materialize.hook.review`. `sync --check` SHALL list pending declaration changes, emit `materialize.hook.review-pending`, write nothing, and exit nonzero. Unchanged declarations SHALL produce no listing, and handwritten declaration files SHALL never be listed.

#### Scenario: Declaration changes are listed for review

- **WHEN** a selected OpenCode hook is added, its command changes, or it is deselected
- **THEN** synchronization SHALL list the add, replace, or remove with the exact commands and emit `materialize.hook.review`
- **AND** `sync --check` SHALL list the pending change, emit `materialize.hook.review-pending`, write nothing, and exit nonzero, while an unchanged sync lists nothing

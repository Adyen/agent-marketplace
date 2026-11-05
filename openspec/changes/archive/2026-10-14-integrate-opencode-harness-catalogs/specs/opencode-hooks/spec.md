## MODIFIED Requirements

### Requirement: Hook contributions are additive and source-bound

Supported hook contributions SHALL retain deterministic ordering, selection identity, and matcher grouping across registries, collections, scopes, and both materialization modes. Removing one selection SHALL retain contributions still selected by another scope or selector. Hook support files SHALL NOT be installed, and sibling files of a source hook definition SHALL NOT be read or installed as support or as additional declarations. A command that references a plugin root or a project directory SHALL be refused with severity-error `hook.opencode.unsupported` together with the hook catalog's refusal code, SHALL emit no declaration, and SHALL NOT point into the Agent Marketplace cache or a source directory. A refusal SHALL preserve previously installed affected hooks, the adapter, and their ownership proof, and SHALL permit only safely independent valid work.

#### Scenario: Removing one contribution preserves the others

- **WHEN** multiple sources contribute supported hooks and one selected contribution is removed
- **THEN** only that contribution's declaration SHALL be removed
- **AND** remaining contributions SHALL execute in the documented order alongside unrelated native plugins

#### Scenario: Support refusal retains the installed hook generation

- **WHEN** a selected hook update references a plugin root or project directory in its command
- **THEN** synchronization SHALL report `hook.opencode.unsupported` and the catalog refusal code, and emit no declaration for that hook
- **AND** the installed declaration, the adapter, and their ownership proof SHALL remain unchanged

#### Scenario: Sibling source files are never installed

- **WHEN** a selected hook's source directory also contains scripts, data files, or other hook declarations
- **THEN** synchronization SHALL install only the selected hook's declaration and the fixed adapter
- **AND** no sibling file SHALL be copied, linked, or activated

## ADDED Requirements

### Requirement: Hook translation follows the shipped catalogs

OpenCode hook events, tool matchers, deadlines, and command variables SHALL be translated from the hook's source harness through the shipped hook and tool catalogs. A matcher SHALL be emitted only when the OpenCode tools it selects cover exactly the source tool set; a file-modification matcher that does not also cover `apply_patch` SHALL be refused with severity-error `hook.opencode.unsupported`. Declarations SHALL spell each tool as the fixed adapter compares it. Ingested declaration matchers SHALL be empty, `*`, or exact tool names joined by `|` that the adapter does not rewrite; any other matcher SHALL be refused with severity-error `ingest.opencode.invalid-resource`.

#### Scenario: Hooks from other harnesses translate through the catalogs

- **WHEN** a native pre-tool hook from another source layout matches that harness's shell tool and is compiled for OpenCode
- **THEN** the declaration SHALL use event `PreToolUse` and matcher `Bash`

#### Scenario: Partial file-modification matchers are refused

- **WHEN** a hook matching only `Edit` or only `Write` is compiled for OpenCode
- **THEN** compilation SHALL report `hook.opencode.unsupported` and emit no declaration
- **AND** a hook matching `Edit|Write` SHALL emit a declaration matching `Edit`, `Write`, and `apply_patch`

#### Scenario: Ambiguous declaration matchers are refused at ingestion

- **WHEN** an OpenCode declaration matcher is regex-like, comma-separated, or names a native tool ID the adapter rewrites before matching
- **THEN** ingestion SHALL report `ingest.opencode.invalid-resource` and import no hook

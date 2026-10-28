## ADDED Requirements

### Requirement: Relocated Claude operations retain native output boundaries

When `CLAUDE_CONFIG_DIR` is set, marker-based cleanup SHALL inspect only aggregate paths declared for the Claude Code harness target, through admitted parent and regular-file authority. A marker in another file beneath that directory SHALL NOT by itself authorize Claude cleanup to read, rewrite, or prune that file. Empty component plans SHALL retain this restriction while still pruning actual stale Claude output and preserving shared permissions. An admitted machine-root directory alias SHALL support native hook configuration and support publication in both materialization modes, retaining the existing source, destination, ownership and generation checks.

Root-overlap comparison SHALL recognize Unicode case variants as well as canonical normalization variants, including prospective paths whose case relationship cannot yet be established through existing filesystem objects.

#### Scenario: Relocated cleanup ignores unrelated files

- **WHEN** a relocated Claude Code root contains stale selected output and unrelated JSON files, including another account's configuration or another repository's MCP configuration, whose entries carry Agent Marketplace markers
- **THEN** synchronization MUST prune the stale Claude resource and aggregate entries while preserving unrelated bytes, inode, permissions and modification time in both materialization modes

#### Scenario: Admitted root alias supports native hooks

- **WHEN** `CLAUDE_CONFIG_DIR` names an admitted directory alias and machine-scope selections include native Claude Code hooks and MCP servers
- **THEN** synchronization MUST install native hook settings, support and MCP entries through that alias in both materialization modes without weakening filesystem boundary checks
- **AND** a subsequent read-only synchronization check MUST report no drift

#### Scenario: Relocated cleanup preserves an unsafe aggregate parent

- **WHEN** a declared aggregate's admitted component directory is retargeted to an unrelated directory during stale-output pruning
- **THEN** synchronization MUST refuse the unsafe path before reading or rewriting the foreign aggregate, preserving its bytes and metadata
- **AND** restoring the original component MUST permit actual stale-entry cleanup

#### Scenario: Prospective Unicode case variants refuse

- **WHEN** repository and machine Claude Code roots differ only by Unicode case variants such as uppercase sigma and final sigma, or ASCII S and long S, and the roots do not yet exist
- **THEN** destination preflight MUST refuse with `materialize.destination.overlap` before either root is created

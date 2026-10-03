## Purpose

Defines OpenCode native destinations, safe shared JSON/JSONC MCP reconciliation, and ownership-preserving inspection, backup, and repair across installation scopes.

## ADDED Requirements

### Requirement: Destinations follow native scope and configuration precedence

Repository resources SHALL default to `.opencode` native directories and machine resources to the supported XDG OpenCode configuration directory. MCP entries SHALL use the applicable native `opencode.json` or `opencode.jsonc` configuration file. Root resolution SHALL match the supported native version, distinguish configuration-file and resource-directory overrides, and report unsafe or ambiguous destinations before affected mutation. Agent Marketplace SHALL NOT create a competing configuration file that masks existing native configuration or manage a broad repository/HOME/configuration tree as output.

#### Scenario: Native JSONC and custom destinations remain effective

- **WHEN** OpenCode has an existing applicable JSONC file or a supported configuration-file/resource-directory override
- **THEN** installation SHALL use the effective native destinations without creating a competing file or redirecting unrelated resources
- **AND** status, doctor, backup, removal, and read-only checking SHALL resolve the same destinations

#### Scenario: Ambiguous destination resolution preserves existing output

- **WHEN** selected OpenCode overrides or native configuration precedence cannot yield an unambiguous authorized destination
- **THEN** the operation SHALL exit nonzero before affected writes and retain configuration and ownership bytes

### Requirement: Both materialization modes preserve scope authority

OpenCode native resource files and trees SHALL support both materialization modes where their native shape permits. Roots, output ancestors/leaves, support sources, and ownership manifests SHALL obey the existing materialization containment and captured-generation requirements. Unsafe aliases, hard links, special files, or changed generations SHALL refuse before unauthorized reads or publication while retaining supported contained and user-owned aliases. Existing same-name unmanaged native definitions, including config-defined and built-in agents, SHALL NOT be silently shadowed.

#### Scenario: Native installation and repetition preserve supported aliases

- **WHEN** repository or machine scope uses supported contained or user-owned aliases and valid selected native resources
- **THEN** both materialization modes SHALL install at the intended native paths and preserve aliases, source content, existing modes, and unchanged-output modification times
- **AND** check-only inspection SHALL report no drift after successful synchronization

#### Scenario: Unsafe destination or payload preserves external state

- **WHEN** a native root, ancestor, leaf, support source, or ownership manifest violates the applicable filesystem boundary or changes generation before publication
- **THEN** the actual consumer SHALL report its stable boundary diagnostic and preserve installed output and external bytes, identity, mode, and modification time
- **AND** it SHALL clean only attempt-owned staging without importing foreign payload or ownership claims

#### Scenario: Effective native identity collisions do not shadow handwritten agents

- **WHEN** desired native output conflicts with an unmanaged directory/config definition or a built-in agent identity whose effective role would be changed
- **THEN** the affected resource SHALL fail with a stable severity-error diagnostic and preserve the existing definition and effective role
- **AND** valid noncolliding resources SHALL remain eligible for installation

### Requirement: Overlapping scopes reconcile only compatible work

Repository, machine, and custom destinations that identify the same native output SHALL be preflighted together. Compatible desired contributions SHALL reconcile once and retain every active selection. Conflicting values, incompatible materialization modes, and scope-restricted operations that cannot isolate the requested scope SHALL report `materialize.destination.overlap` before mutation.

#### Scenario: Shared native destinations retain both scopes

- **WHEN** compatible repository and machine selections share one canonical OpenCode destination
- **THEN** synchronization SHALL retain both scopes' desired resources and handwritten content without duplicate publication or no-op rewrites
- **AND** removing a safely separable selection SHALL preserve resources still desired by another selection

#### Scenario: Ambiguous overlapping scope operations refuse

- **WHEN** shared destinations have conflicting desired values or an operation cannot isolate its requested scope
- **THEN** the operation SHALL report `materialize.destination.overlap` and preserve native output and ownership state

### Requirement: MCP entries use durable ownership and faithful patching

Only proven owned `mcp.<name>` entries SHALL be reconciled or pruned. Ownership state SHALL remain outside the repository and disposable cache and SHALL NOT embed executable or credential values in ownership records. No ownership marker SHALL be written into native configuration. Missing/corrupt ownership state SHALL preserve existing entries, and unmanaged collisions SHALL report `ownership.unmanaged-key`. JSON/JSONC patching SHALL preserve comments, formatting, key order, unrelated settings, handwritten entries, and existing permissions; malformed or unpatchable input SHALL refuse the affected aggregate without blocking safely independent resources.

#### Scenario: MCP update preserves handwritten JSONC

- **WHEN** a proven owned MCP entry changes in JSONC containing handwritten entries, comments, permission rules, providers, and unrelated settings
- **THEN** synchronization SHALL update only authorized content and preserve the handwritten bytes, ordering, and existing file permissions
- **AND** repeat synchronization SHALL preserve bytes and modification time without adding ownership markers

#### Scenario: Missing ownership and collisions preserve user entries

- **WHEN** ownership state is missing/corrupt or a desired MCP identity collides with an unmanaged entry
- **THEN** existing entries SHALL remain unchanged and unproven entries SHALL NOT be pruned
- **AND** collisions SHALL report `ownership.unmanaged-key` while independent valid work follows the existing scoped failure contract

#### Scenario: Malformed shared configuration refuses affected mutation

- **WHEN** an existing native aggregate has duplicate/ambiguous keys, invalid JSONC, or an unpatchable native form
- **THEN** the aggregate mutation SHALL fail with a stable error and preserve aggregate and ownership bytes
- **AND** independent valid native resources SHALL still materialize when safe

### Requirement: Publication is private journaled and generation-checked

New sensitive aggregates, staging, and mandatory raw-byte safety backups SHALL have private permissions. Existing shared-file modes SHALL survive compilation, fragment merging, direct conversion, and native publication, and incompatible requested modes SHALL refuse. Aggregate read/derive/recovery/publication SHALL be serialized per canonical destination and generation-checked with journaled recovery. Mandatory backups SHALL remain enabled even when optional backups are disabled. Identical desired output SHALL cause no rewrite or chmod.

#### Scenario: Creation and conversion retain private modes

- **WHEN** synchronization or direct conversion creates sensitive OpenCode MCP output under a permissive process umask
- **THEN** output and mandatory safety backups SHALL remain owner-readable only throughout publication
- **AND** updating an existing file SHALL preserve its mode and repeating unchanged output SHALL preserve bytes and modification time

#### Scenario: Concurrent or interrupted updates preserve an authorized generation

- **WHEN** two processes update one native aggregate or a process stops during journaled publication
- **THEN** updates SHALL serialize or refuse with `ownership.generation-conflict` rather than mix aggregate and ownership generations
- **AND** recovery SHALL retain handwritten content and refuse unrelated foreign changes

### Requirement: Checking status and diagnostics remain read-only

`sync --check`, status, and diagnostic-only doctor SHALL detect native drift and unsafe destinations without executing native resources or creating/changing configuration, output directories, backups, mutation locks, or ownership state. Backup inventories SHALL include only explicitly resolved applicable native resources and aggregates.

#### Scenario: Read-only inspection creates no native or ownership state

- **WHEN** checking, status, or diagnostic-only doctor inspects missing, drifted, or unsafe OpenCode destinations
- **THEN** it SHALL report attributable drift or boundary diagnostics without creating or changing destination, backup, mutation-lock, or ownership files
- **AND** safe synchronized destinations SHALL report their healthy state

#### Scenario: Machine backup uses explicit native inventory

- **WHEN** a machine backup includes OpenCode with supported custom native destinations
- **THEN** it SHALL archive the explicit applicable native resources and configuration mapping with private archive permissions
- **AND** it SHALL NOT traverse unrelated HOME or configuration contents

### Requirement: Repair retains exact acquisition and fresh policy authority

OpenCode shared-configuration repair SHALL use the existing disjoint adoption/replacement/orphan candidate rules and verified exact acquisition authority. `--yes` SHALL permit only byte-neutral adoption; destructive replacement/removal SHALL require interactive confirmation and fresh private backup. Confirmation SHALL hold no mutation lock. Configuration, acquisition locks, and policy SHALL be revalidated after confirmation and at publication; stale candidates SHALL refuse with nonzero exit, retain aggregate/ownership bytes, and discard attempt-owned staging.

#### Scenario: Adoption is byte-neutral and destructive repair requires consent

- **WHEN** `doctor --repair --yes` encounters an exact desired match and an unmanaged differing value
- **THEN** it SHALL adopt the exact match without rewriting native bytes and SHALL NOT replace or remove the differing value without interactive confirmation
- **AND** confirmed destructive repair SHALL take a private backup and preserve unrelated content

#### Scenario: Stale repair authority refuses publication

- **WHEN** repair acquisition evidence is invalid or configuration, acquisition locks, or policy change during confirmation, staging, or publication
- **THEN** repair SHALL refuse the stale action without changing aggregate, ownership, or acquisition-lock bytes and remove attempt-owned staging
- **AND** valid unchanged exact acquisition evidence SHALL permit the confirmed intended repair without holding a mutation lock during its prompt

## Purpose

Reserves MCP publishing metadata at the native serialization boundary without altering intrinsic content, historical source revisions, or connection behavior.

## ADDED Requirements

### Requirement: Native MCP settings exclude publishing metadata

Every harness target at repository and machine scopes SHALL omit `_agent-marketplace` and its case variants from native MCP server settings, including when the value is malformed, empty, or supplied through preserved canonical extension fields. Publishing metadata alone SHALL NOT suppress an otherwise valid connection or produce an unsupported-field warning. The compiler SHALL NOT mutate the resource or install a referenced publishing README. Other native connection fields and unrelated extensions, including a bare native description, SHALL retain their established target-specific behavior.

#### Scenario: Every target consumes the reserved namespace

- **WHEN** stdio and remote MCP resources with publishing namespace entries are compiled and materialized for all seven harness targets at both scopes
- **THEN** native artifacts SHALL match equivalent resources without those entries, contain no publishing namespace or its document text, and create no publishing README file
- **AND** valid connections and established unrelated native fields SHALL remain present

#### Scenario: Malformed and case variant entries cannot bypass exclusion

- **WHEN** publishing namespace entries have null, scalar, empty, or forged values, use case variants, or coexist under several case variants in preserved extensions
- **THEN** none SHALL enter native MCP settings or cause a publishing-only unsupported-field warning
- **AND** the original preserved extension map SHALL remain unchanged

### Requirement: Native exclusion preserves intrinsic content and compatibility

The publishing namespace SHALL remain preserved by existing canonical encoding and decoding; native exclusion SHALL NOT change canonical payload bytes, content digests, or verification. Existing metadata-free resources, source-revision generators v1 through v4, current writer defaults, and healthy acquisition locks SHALL retain their established behavior. This prerequisite SHALL NOT enable a new source-revision format or claim that released clients can read one.

#### Scenario: Canonical restoration retains content while native output excludes metadata

- **WHEN** a verified canonical MCP artifact contains publishing metadata with captured documentation text
- **THEN** decoding and re-encoding SHALL preserve its canonical payload and digest
- **AND** native compilation of the restored resource SHALL exclude publishing metadata without mutating the decoded content

#### Scenario: Historical identity and writer defaults remain unchanged

- **WHEN** metadata-free and pre-feature extension-bearing resources are processed through canonical encoding and historical source-revision verification
- **THEN** frozen payloads and supported historical digest vectors SHALL remain unchanged
- **AND** no new source-revision version SHALL become the default writer

### Requirement: Synchronization retains its existing no-op and ownership boundaries

Synchronizing a valid selected MCP resource with publishing-only changes SHALL produce the same desired native connection entry. Repeat synchronization and read-only checking SHALL preserve healthy native settings, unmanaged siblings, ownership authority, and acquisition-lock bytes according to their existing contracts.

#### Scenario: Publishing only changes do not rewrite healthy native settings

- **GIVEN** an installed MCP server and an unrelated handwritten sibling
- **WHEN** an authorized update changes only publishing metadata, followed by repeat sync, locked sync, and read-only checking
- **THEN** the native settings bytes and modification time SHALL remain unchanged and the sibling SHALL remain unowned
- **AND** repeat, locked, and read-only operations SHALL NOT rewrite the healthy acquisition lock or acquire new prune authority

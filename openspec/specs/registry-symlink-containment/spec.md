# registry-symlink-containment Specification

## Purpose
Define when symlinks in a Git registry repository affect exact snapshot acquisition and how the configured source root constrains source revision validation.

## Requirements

### Requirement: Exact snapshot acquisition is independent of unselected symlinks

An exact snapshot SHALL remain acquirable and reusable when its verified Git tree contains dangling or escaping symlinks outside the configured source root. Exact tracked-tree integrity checks MUST still reject mismatched content.

#### Scenario: Snapshot contains unselected unsafe symlinks

- **WHEN** a registry repository's exact commit contains dangling and escaping symlinks outside the configured source root
- **THEN** acquisition and reuse of the exact snapshot MUST succeed when its tracked tree matches the commit

#### Scenario: Snapshot tracked tree is altered

- **WHEN** a cached exact snapshot is opened read-only after its tracked contents differ from its commit
- **THEN** the open MUST reject the snapshot as untrusted, irrespective of symlink validation scope

### Requirement: Configured source root bounds symlink validation

Source revision creation SHALL accept symlinks outside the configured source root without validating their destinations. It MUST reject unresolved symlinks within that root and symlinks within it that resolve outside that root, without incorporating those targets into the source revision.

#### Scenario: Narrowed source root excludes unsafe symlink

- **WHEN** a configured source root excludes a dangling symlink elsewhere in the same registry repository
- **THEN** source revision creation for that root MUST succeed while creation for a root containing the link MUST fail

#### Scenario: Selected unsafe symlink identifies remediation

- **WHEN** a symlink inside the configured source root is dangling or resolves outside that root
- **THEN** source revision creation MUST fail with a path escape error that identifies the registry-relative link path, its authored target, the configured root, and remediation to remove the link or narrow the configured path

### Requirement: Disk primary payload ingestion stays below captured source authority

Agent Marketplace MUST read disk-backed catalog primary payloads and bundle detection payloads through a captured source root. Payloads MUST be regular singly-linked opened snapshots with stable identity/size and captured-size streaming/growth checks, not path-based reopen after an earlier metadata check. Foreign/unsafe aliases and special files MUST refuse before their bytes enter resource parsing; FIFO opens MUST not block. Direct native catalog loading MUST retain valid sibling listing and its coded refusal diagnostics, while publication discovery MUST propagate typed source refusals rather than downgrade them to syntax warnings and prune the installed generation. Both MUST preserve original/external bytes, identity, permissions and modification time. Contained relative primary file aliases MUST remain supported. No new absolute byte/tree/work budget or whole-mutable-tree atomic snapshot is introduced.

#### Scenario: Primary ingestion refuses foreign payload aliases

- **WHEN** a native disk catalog primary resource aliases a payload outside its captured source root
- **THEN** ingestion MUST not import that payload, MUST retain its diagnostic and valid sibling listing, and MUST preserve external data and metadata

#### Scenario: Contained primary file aliases retain authored content

- **WHEN** a primary file aliases regular authored content within the captured source root
- **THEN** native ingestion MUST retain its authored name/description without modifying the source, while a multiply linked payload MUST refuse

### Requirement: Source revision payload hashes use captured read authority

Agent Marketplace MUST hash primary and intrinsic support payloads below a captured revision source root using descriptor-bound regular singly-linked nonblocking snapshots with captured-size streaming/growth and stable-state checks. Payload boundary refusals, including typed ingestion refusals, MUST fail revision creation as hard coded/typed errors rather than canonical-primary fallback or omitted-resource warnings, and MUST produce no partial manifest payload. Existing compatibility schemas and bytes, canonical collection serialization, configured-root checks, established link policy and genuine optional absence behavior MUST remain intact. No new absolute input/tree/work ceiling or whole-mutable-source atomic snapshot is introduced.

#### Scenario: Source revision hashing refuses post-ingest unsafe payloads

- **WHEN** an admitted primary or intrinsic support payload becomes multiply linked before actual source-revision hashing
- **THEN** Build MUST fail with the typed unsafe-file identity, produce no partial revision payload and preserve original source bytes/identity/mode/mtime, while valid deterministic/compatibility builds continue

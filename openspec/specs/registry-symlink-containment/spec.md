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

### Requirement: Disk primary directory aliases retain contained source layout compatibility

Agent Marketplace MUST accept contained source directory aliases and read primary payload paths beneath contained aliased ancestors without leaving the captured source root. Directory enumeration MUST use captured directory authority, and payloads MUST retain regular singly-linked opened-generation and captured-size growth checks. A missing optional descendant beneath a valid contained directory alias MUST remain absence rather than a source refusal. Dangling, cyclic, escaping, and leave-and-return directory aliases MUST refuse without importing foreign bytes or changing source/external state. Publication discovery MUST retain typed source refusal rather than treating unsafe input as an empty source layout. Existing destination alias policy is unchanged.

#### Scenario: Contained primary directory aliases retain Factory catalog content

- **WHEN** a native Factory workspace and command directory use contained directory aliases with a valid sibling skill
- **THEN** catalog loading and publication discovery MUST retain the authored command and sibling skill without changing payload metadata or treating absent optional descendants as a source refusal

#### Scenario: Source directory alias reads retain captured root authority

- **WHEN** source directory aliases and alias chains are opened, enumerated, and read beneath a captured root whose configured path is subsequently replaced
- **THEN** directory and payload reads MUST retain the captured contained content rather than use the replacement root, including regular executable file aliases beneath the contained ancestors

#### Scenario: Unsafe Factory directory aliases refuse publication

- **WHEN** a native Factory command directory aliases an escaped, dangling, or leave-and-return target
- **THEN** publication discovery MUST propagate a typed unsafe-path refusal, native loading MUST not import foreign commands, and external bytes, identity, permissions, and modification time MUST remain unchanged

#### Scenario: Source directory alias refusals retain payload shape checks

- **WHEN** a source directory alias escapes, dangles, cycles, leaves and returns to the root, or exposes multiply-linked payloads
- **THEN** source payload reads MUST refuse with the applicable typed unsafe-path or unsafe-file identity, retain that refusal for publication, preserve external bytes and metadata, and continue to permit independent valid singly-linked sibling reads

### Requirement: Source revision payload hashes use captured read authority

Agent Marketplace MUST hash primary and intrinsic support payloads below a captured revision source root using descriptor-bound regular singly-linked nonblocking snapshots with captured-size streaming/growth and stable-state checks. Payload boundary refusals, including typed ingestion refusals, MUST fail revision creation as hard coded/typed errors rather than canonical-primary fallback or omitted-resource warnings, and MUST produce no partial manifest payload. Existing compatibility schemas and bytes, canonical collection serialization, configured-root checks, established link policy and genuine optional absence behavior MUST remain intact. No new absolute input/tree/work ceiling or whole-mutable-source atomic snapshot is introduced.

#### Scenario: Source revision hashing refuses post-ingest unsafe payloads

- **WHEN** an admitted primary or intrinsic support payload becomes multiply linked before actual source-revision hashing
- **THEN** Build MUST fail with the typed unsafe-file identity, produce no partial revision payload and preserve original source bytes/identity/mode/mtime, while valid deterministic/compatibility builds continue

### Requirement: Native collection definitions remain within captured source authority

Agent Marketplace MUST discover native collection definitions below a captured registry source root and parse only descriptor-bound regular singly-linked nonblocking snapshots with captured-size growth and stable-state checks. Unsafe or foreign collection payloads MUST return typed source refusals, MUST NOT contribute selection metadata or trigger native output pruning, and MUST preserve source/external bytes and identity/mode/mtime. Contained authored file aliases, lexical provenance, deterministic ordering, established flat/non-directory filtering and genuine optional absence MUST remain supported. Unsafe dangling metadata MUST NOT be treated as optional absence. No new absolute byte/tree/work budget or whole-mutable-source atomic snapshot is introduced.

#### Scenario: Collection source refusal preserves authority

- **WHEN** a native collection payload aliases a foreign file or becomes a nonregular/multiply linked unsafe payload
- **THEN** discovery MUST refuse before import, preserve the source/external generation and installed native output, while valid collection and contained-file-alias controls retain selection/provenance

### Requirement: Legacy metadata prefix reads retain captured source authority

Agent Marketplace legacy skill frontmatter and best-effort authored-name recovery MUST use captured source directory/destination authority and opened regular singly-linked nonblocking files. Existing prefix readers MUST retain scanner limits and early termination without reading whole bodies or adding new arbitrary size policies. Streaming MUST be bounded to captured size and stable opened-state checks MUST reject mutation during the callback. Registry discovery and malformed correlation MUST use their admitted registry root; unsafe aliases or multiply linked payloads MUST NOT supply foreign resource metadata. Syntax/malformed policy, existing standalone helpers and contained authored file aliases MUST remain compatible. No whole-mutable-source atomic snapshot is introduced.

#### Scenario: Legacy metadata readers refuse foreign source payloads

- **WHEN** a native legacy skill's primary metadata aliases a foreign or multiply linked payload
- **THEN** legacy discovery MUST not import its metadata and best-effort recovery MUST not report its authored name, preserving source/external bytes and metadata while valid contained-file-alias/prefix controls remain supported

### Requirement: Filesystem source previews use captured opened-file authority

Agent Marketplace filesystem detail-source capture and selected resource bundle previews MUST use descriptor-bound reads from their authorized containing directory or resource root. Opened payloads MUST be regular and singly linked, FIFO opens MUST not block, and opened state plus captured target generations MUST remain validated through the read. Final source symlinks MUST retain the existing no-follow refusal. Refused reads MUST not contribute payload bytes or mutate source/external generations. Existing per-source and aggregate capture limits, truncation markers, archive-backed previews and rendering behavior MUST remain supported without new processing budgets or whole-body buffering. No whole-source atomic snapshot is introduced.

#### Scenario: Detail source previews refuse multiply linked payloads

- **WHEN** an otherwise valid filesystem source preview becomes multiply linked or nonregular
- **THEN** source capture and selected bundle previews MUST refuse without importing payload bytes or blocking on a FIFO, preserve original file metadata/bytes, and retain the valid bounded/truncated preview controls

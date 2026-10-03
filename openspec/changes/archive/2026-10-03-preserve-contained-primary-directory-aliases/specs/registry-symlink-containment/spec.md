## ADDED Requirements

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

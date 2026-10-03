## ADDED Requirements

### Requirement: Native collection definitions remain within captured source authority

Agent Marketplace MUST discover native collection definitions below a captured registry source root and parse only descriptor-bound regular singly-linked nonblocking snapshots with captured-size growth and stable-state checks. Unsafe or foreign collection payloads MUST return typed source refusals, MUST NOT contribute selection metadata or trigger native output pruning, and MUST preserve source/external bytes and identity/mode/mtime. Contained authored file aliases, lexical provenance, deterministic ordering, established flat/non-directory filtering and genuine optional absence MUST remain supported. Unsafe dangling metadata MUST NOT be treated as optional absence. No new absolute byte/tree/work budget or whole-mutable-source atomic snapshot is introduced.

#### Scenario: Collection source refusal preserves authority

- **WHEN** a native collection payload aliases a foreign file or becomes a nonregular/multiply linked unsafe payload
- **THEN** discovery MUST refuse before import, preserve the source/external generation and installed native output, while valid collection and contained-file-alias controls retain selection/provenance

## ADDED Requirements

### Requirement: Legacy metadata prefix reads retain captured source authority

Agent Marketplace legacy skill frontmatter and best-effort authored-name recovery MUST use captured source directory/destination authority and opened regular singly-linked nonblocking files. Existing prefix readers MUST retain scanner limits and early termination without reading whole bodies or adding new arbitrary size policies. Streaming MUST be bounded to captured size and stable opened-state checks MUST reject mutation during the callback. Registry discovery and malformed correlation MUST use their admitted registry root; unsafe aliases or multiply linked payloads MUST NOT supply foreign resource metadata. Syntax/malformed policy, existing standalone helpers and contained authored file aliases MUST remain compatible. No whole-mutable-source atomic snapshot is introduced.

#### Scenario: Legacy metadata readers refuse foreign source payloads

- **WHEN** a native legacy skill's primary metadata aliases a foreign or multiply linked payload
- **THEN** legacy discovery MUST not import its metadata and best-effort recovery MUST not report its authored name, preserving source/external bytes and metadata while valid contained-file-alias/prefix controls remain supported

## ADDED Requirements

### Requirement: Support preparation and writes share captured output authority

Agent Marketplace MUST retain one captured output directory for eligible support traversal. Descendant directory preparation, producer exclusive file writes and snapshot publication MUST remain relative to that authority, and directory capture MUST refuse a named-root generation change before mutation. Refusal MUST preserve foreign bytes/metadata/members and report materialize.write.error without native installation. Existing source modes and contained input aliases remain compatible; empty or excluded-only support MUST NOT create a missing output. This requirement does not establish an atomic cache/native transaction or producer namespace creation authority before copy admission.

#### Scenario: Support output refuses retarget before directory and file writes

- **WHEN** public native sync retargets admitted producer output before support traversal
- **THEN** preparation/writes MUST refuse without creating foreign directories/files or changing foreign bytes/inode/mode/mtime, keep native absent, and a healthy retry MUST actually create nested native support with exact source bytes/mode and clean its new staging

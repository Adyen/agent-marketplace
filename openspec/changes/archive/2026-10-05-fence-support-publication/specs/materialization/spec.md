## ADDED Requirements

### Requirement: Support-file publication retains admitted destination and staging generations

Agent Marketplace support snapshot publication MUST use the captured publisher for its classified destination generation. Parent, destination leaf and staged-file generation changes MUST refuse before rename, preserve replacement bytes and metadata, and remove only the attempt-created stage through its captured parent. Native sync MUST report materialize.write.error and MUST NOT install the refused cache-backed skill. Existing source mode and byte/mode no-op behavior remain compatible. This requirement does not make support-directory preparation, producer cleanup and native publication one transaction.

#### Scenario: Support publication refuses replaced parent and stage

- **WHEN** public native sync replaces the support destination parent after staging and places a foreign staged file at the same basename through the replacement alias
- **THEN** support publication MUST refuse, preserve foreign stage bytes/inode/mode/mtime/membership, clean its original stage without native installation, and restoring original parent MUST permit real byte-exact native support publication with its mode

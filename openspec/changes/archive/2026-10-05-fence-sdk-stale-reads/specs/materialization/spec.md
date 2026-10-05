## ADDED Requirements

### Requirement: SDK stale payload readers admit stable singly linked regular files

SDK stale-candidate digest reads MUST remain under captured parent authority, match original full file state and reject leaf aliases, nonregular or multiply linked payloads before hashing. Quarantine verification MUST open nonblocking and validate the opened regular singly linked file before reading; observed size and descriptor stability MUST bound all stale digests. Reader/close refusals MUST preserve original error identity, foreign payload bytes/metadata and existing quarantine restoration. Valid regular stale pruning and truthful removal reporting MUST remain functional. This requirement does not independently fence directory discovery, deletion generations or empty-parent cleanup.

#### Scenario: SDK stale reads refuse multiply linked payloads

- **WHEN** public SDK clean sync finds an independently authored stale regular file with an extra foreign hard link
- **THEN** it MUST refuse unsafe-file admission before staging or deletion, preserve both names/bytes/inode/mode/mtime/output membership, and removing the extra link MUST permit actual regular stale prune

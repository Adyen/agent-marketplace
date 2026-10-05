## ADDED Requirements

### Requirement: SDK publication retains root and staging-directory authority

SDK Materialize MUST retain admitted output-root and attempt staging-directory objects through staged publication. Replacing either before publication MUST refuse with generation conflict before consuming foreign payloads. Destination parent creation and rename MUST remain descriptor-relative to captured output authority. Attempt cleanup MUST remove only its original directory inode, preserve replacement members/bytes/metadata and propagate refusal rather than report successful publication. Existing staged-all-artifacts behavior, modes, dry-run and partial-result semantics remain compatible. Initial stage preparation/writes and individual file-generation leases are not established by this requirement.

#### Scenario: SDK publication refuses replaced root and preserves foreign stage

- **WHEN** public SDK Materialize replaces output root after flat or nested staging and a foreign directory contains staged payload at the original basename
- **THEN** it MUST refuse before consuming foreign stage, preserve foreign bytes/inode/mode/mtime/membership, remove only original attempt stage, and healthy publication to original root MUST really write source-mode bytes without staging residue

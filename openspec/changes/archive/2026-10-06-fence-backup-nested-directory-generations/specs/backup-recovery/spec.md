## ADDED Requirements

### Requirement: Nested archive directory descent retains admitted child authority

Ordinary and Codex backup MUST match each classified child directory to a descriptor opened below its captured admitted parent before reading members, use that child descriptor for descendant metadata/payload reads, and validate original named child object before success. Replaced child or captured-ancestor generations MUST refuse rather than authorize foreign payload import. Refusal MUST preserve original/foreign nested directory/payload bytes/inodes/modes/mtimes and leave no archive/reservation residue. Sorted original tar hierarchy, root-dot omission, symbolic text/modes, initial source aliases and actual valid recovery MUST remain supported. Active directory descriptors remain held by recursion; OS resource exhaustion MUST propagate rather than authorize success. No new arbitrary depth/work limit, complete-tree content freeze, archive transaction or atomic exclusion is established.

#### Scenario: Nested backup directory generation refuses foreign descent

- **WHEN** real public ordinary or Codex creation substitutes a classified nested child directory at descent or after opening with a foreign directory/member while the selected root remains original
- **THEN** creation MUST generation-refuse preserving original/foreign bytes/metadata with no archive/reservation residue, and restoring original child authority MUST genuinely create an archive whose recovery restores original bytes without foreign payloads

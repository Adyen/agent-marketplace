## ADDED Requirements

### Requirement: Journal updates retain their originally published metadata generation

Agent Marketplace journal phase and staged-collision disarm publication MUST remain bound to the previously published journal observation. Original full journal state and captured parent object MUST validate before namespace preparation and again in staged metadata publication. An identical JSON replacement inode MUST NOT authorize overwrite/state commit/success; typed refusal MUST preserve original/replacement metadata identity/mode/mtime/bytes and remove only attempt staging. Initial unobserved journal creation, format/private modes and ordinary metadata writes remain compatible. Native already published before a late phase refusal remains published with original prepared recovery evidence; no whole native/metadata transaction, new processing budget or uncooperative-writer exclusion.

#### Scenario: Journal phase update preserves original generation

- **WHEN** public Apply publishes native next bytes and the existing after-aggregate-rename boundary replaces its original prepared journal with identical JSON/private mode
- **THEN** phase publication MUST refuse typed generation before overwriting replacement or committing state, preserve original/replacement/no metadata staging, and restoring original prepared evidence MUST allow real public recovery roll-forward/commit/cleanup

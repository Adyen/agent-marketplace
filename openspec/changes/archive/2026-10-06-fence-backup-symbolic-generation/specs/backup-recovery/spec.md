## ADDED Requirements

### Requirement: Symbolic restore retains admitted destination leaf generations

Ordinary and Codex symbolic restore MUST compare current no-follow leaf state or absence with the admitted generation immediately before replacement removal below captured parent authority. Later/unknown leaf generations MUST generation-refuse and remain untouched with foreign subtree members and retained original bytes/metadata preserved. Existing regular/single-link structural admission, directory/link/absence replacement behavior and exact archived target creation MUST remain functional. Codex's earlier existing-symlink preflight refusal MUST remain intact. This rule does not bind late named-parent authority, recursively snapshot descendant generations or establish transactional/atomic uncooperative-writer exclusion.

#### Scenario: Symbolic restore preserves late foreign destination generation

- **WHEN** real public ordinary or Codex restore replaces an admitted symbolic-destination leaf or absence at its replacement boundary with a foreign directory containing an inert member
- **THEN** restore MUST generation-refuse without changing foreign tree/member and original bytes/inodes/modes/mtimes/root membership, and restoring admitted authority MUST genuinely recover exact archived symbolic target while earlier foreign tree stays unchanged

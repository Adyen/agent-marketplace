## ADDED Requirements

### Requirement: Codex backup-set publication retains attempt authority

Agent Marketplace Codex backup-set creation MUST retain captured set-directory authority through operation-name reservation, partial archive/manifest creation, final publication and refusal cleanup. Existing final or partial manifest names, including aliases, MUST remain occupied rather than being truncated. New members MUST be exclusively created privately; producer-observed regular/single-link identity/state, final absence and parent object MUST be validated before descriptor-relative publication. Cleanup MUST remove only tracked attempt-created inodes and preserve foreign replacements. Manifest-last completeness, exact root keys, format and archive0600/manifest0644 modes MUST remain supported. No new budget, durability sync, whole-set transaction or uncooperative-writer exclusion is introduced; namespace preparation and retention remain separate.

#### Scenario: Codex set publication preserves aliased partial manifest

- **WHEN** real public Codex backup-set creation encounters a pre-existing partial manifest alias
- **THEN** foreign bytes and original alias text MUST remain unchanged, the creator MUST use an available operation without attempt partial residue, and the resulting set MUST restore actual archived payload through public restore

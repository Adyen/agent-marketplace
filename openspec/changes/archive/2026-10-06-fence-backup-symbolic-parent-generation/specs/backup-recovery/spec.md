## ADDED Requirements

### Requirement: Symbolic restore requires original named parent authority

Ordinary and Codex symbolic restoration MUST verify that the currently named parent still resolves to the captured directory object at the replacement action boundary before leaf removal and link creation. Detached original descriptors MUST NOT authorize mutation/success after named-parent replacement. Typed refusal MUST preserve original and foreign parent/leaf bytes, identities, modes, mtimes and membership. Supported initial parent aliases and genuine exact symbolic target recovery MUST remain functional. No recursive descendant snapshot, atomic uncooperative-writer exclusion or whole-operation transaction is established by this rule.

#### Scenario: Symbolic restore refuses detached named parent generation

- **WHEN** real public ordinary or Codex symbolic restore replaces the named parent at the mutation boundary while the admitted leaf remains valid under its detached original descriptor
- **THEN** restore MUST generation-refuse before original deletion, preserving original/foreign parent and leaf metadata/bytes/members, and restoring original parent authority MUST genuinely recover exact archived symbolic target without changing retained foreign parent

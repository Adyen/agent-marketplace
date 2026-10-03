## ADDED Requirements

### Requirement: Portable regular-file publication preserves admitted namespace generations

Agent Marketplace portable regular-file output publication MUST stage and publish below captured authorized parent directories using descriptor-relative operations. The observed destination leaf full identity/state and named parent object MUST be revalidated before replacing a file or migrating a managed link; refusal MUST preserve replacement and foreign bytes, the admitted original and manifest bytes, and remove attempt-owned staged names through their captured parent. Cache source reads MUST retain regular/singly-linked/nonblocking opened-file authority and captured-size validation. Valid native updates, existing no-op ownership/mode semantics and atomic-rename durability behavior MUST remain supported. Directory-source publication/removal/pruning and output/manifest transactions remain separate; no uncooperative-writer exclusion or new processing budgets are introduced.

#### Scenario: Portable file publication refuses replaced leaf generation

- **WHEN** an admitted portable native file update encounters a replaced leaf or retargeted parent after staging
- **THEN** materialization MUST report a coded refusal, retain admitted/replacement/foreign/manifest bytes and clean only attempt-owned staging, while valid content updates still publish

## ADDED Requirements

### Requirement: Portable directory publication validates authority before live removal

Agent Marketplace portable directory publication MUST stage source contents through captured source readers and captured destination parent authority before live output removal. Opened source modes MUST remain faithful on staged files. Full observed root generation, saved ownership digest, named parent and staged object authority MUST be rechecked before descriptor-relative removal/rename. Refusal before live removal MUST preserve admitted/replacement/manifest bytes and clean only attempt-owned staged directories. Valid native updates/no-op/migration controls MUST remain supported. Pruning, aggregate/restore/ownership consumers and output/manifest transactions remain separate; directory removal/rename are not transactional and do not exclude uncooperative writers. No new arbitrary processing budget or fsync policy is introduced.

#### Scenario: Portable tree publication preserves replacement generation

- **WHEN** an admitted portable native tree update encounters a replacement tree after complete source staging
- **THEN** materialization MUST report a coded refusal, retain admitted/replacement/manifest bytes and clean staged residue, while valid tree content updates continue publishing

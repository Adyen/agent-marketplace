## ADDED Requirements

### Requirement: Archive generation retains captured output and cleanup authority

Agent Marketplace ordinary and Codex staged archive generation MUST create exclusive private0600 outputs beneath captured existing parent descriptors. Completed writer regular/single-link state MUST match the named archive and admitted parent object before success is reported. Refusal cleanup MUST remove only the attempt-created inode from the captured parent, preserve foreign replacements and propagate meaningful close/cleanup errors. Existing initial root/parent alias admission, exclusivity/collision, tar/gzip format and close-only durability semantics MUST remain supported. Namespace preparation, Codex final/manifest publication and retention remain separate; no new budgets, per-file fsync, whole archive transaction or exclusion of uncooperative writers is introduced.

#### Scenario: Archive output refusal keeps foreign replacement

- **WHEN** a shared archive producer retargets the admitted output parent after creation and then fails
- **THEN** refusal MUST retain the original source error, preserve foreign replacement archive bytes and remove only the attempt inode from the captured parent, while ordinary valid Create/Restore and existing output collision controls MUST still work

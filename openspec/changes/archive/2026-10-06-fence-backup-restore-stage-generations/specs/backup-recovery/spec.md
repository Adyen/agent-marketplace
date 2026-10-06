## ADDED Requirements

### Requirement: Completed restore staged payload authority survives final publication

Regular restore MUST compare the final staged name below captured parent authority with the completed written/chmodded descriptor's full identity/state immediately before relative publication. Missing, aliased or replaced stages MUST generation-refuse without publishing foreign bytes; cleanup MUST preserve unknown replacements and current native output. Ordinary archives, explicitly mapped native files and Codex sets MUST retain genuine byte/mode restoration, including unreadable archived modes without reopening payload after chmod. This control does not independently bind later destination/parent actions or supply transactional extraction or uncooperative-writer exclusion.

#### Scenario: Restore staged payload replacement refuses publication

- **WHEN** public ordinary, explicitly mapped or Codex set restore replaces its actual completed staged file at the final publication boundary with independently authored foreign bytes
- **THEN** restore MUST generation-refuse and preserve current/foreign/retained-stage bytes/inodes/modes/mtimes and output membership, and a healthy retry MUST actually restore archived bytes/mode while preserving the earlier foreign stage

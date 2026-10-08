## ADDED Requirements

### Requirement: Prepared recovery cleanup retains original metadata after native observation

Prepared ownership recovery MUST revalidate original state/journal generations after bounded live aggregate observation and before decisions consume staging/journal evidence. Late same-JSON state replacements or aliases MUST typed-refuse with no successful outcomes and preserve original/foreign bytes/inodes/modes/mtimes/links, valid journal/stage evidence and native prior/handwritten bytes or absence. Restored original authority MUST still perform genuine present/absent rollback or abandonment, retain committed generation and preserve the established foreign-change reporting contract. Ordinary malformed state recovery compatibility MUST remain. No new processing budget, content/durability policy, full transaction rollback, complete snapshot or atomic uncooperative-writer exclusion is established.

#### Scenario: Recovery cleanup rechecks original state after aggregate read

- **WHEN** real prepared recovery replaces original state with a same-JSON regular file or external alias after live aggregate observation before rollback/abandon decisions
- **THEN** recovery MUST refuse preserving original/replacement metadata, stage/journal evidence and native bytes/absence, and restoring original authority MUST genuinely roll back or abandon only the owned prepared generation with the established outcome and diagnostic

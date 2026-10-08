## ADDED Requirements

### Requirement: Empty safety-directory cleanup retains original attempt generation

CAS-refusal safety cleanup MUST revalidate original attempt child identity and named state-parent generation immediately before descriptor-relative Rmdir. Replaced empty/nonempty directories or aliases MUST remain unchanged, preserving original/foreign identity/modes/mtimes/membership/member bytes and native prior state. Original policy/CAS refusal and unsuccessful generation receipt semantics MUST remain, with empty-directory cleanup retaining its existing best-effort warning boundary. Healthy retries MUST genuinely remove original owned empty attempt directories and permit real subsequent generation/native publication. Existing journal/stage/member cleanup, Close errors and ENOTEMPTY/EEXIST behavior MUST remain. No new budget, content/durability policy, complete snapshot, atomic uncooperative-writer exclusion or whole transaction rollback is established.

#### Scenario: Empty safety directory cleanup preserves replacement generation

- **WHEN** actual Apply with a real late policy refusal replaces its admitted emptied safety attempt directory before final Rmdir
- **THEN** cleanup MUST preserve original/foreign directory and member generations/native state with the primary refusal intact, and restoring original authority MUST genuinely remove the owned empty directory and allow an actual native generation commit

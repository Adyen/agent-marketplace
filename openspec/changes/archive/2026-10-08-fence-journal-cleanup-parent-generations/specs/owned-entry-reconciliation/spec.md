## ADDED Requirements

### Requirement: Journal cleanup retains named metadata-parent generation

Ownership journal deletion MUST verify that its admitted captured parent is still the named canonical metadata parent immediately before removal. Real-directory replacements or late parent aliases MUST typed-refuse without a successful Apply result/Recover outcome and preserve original/foreign state+journal bytes/inodes/modes/mtimes, directory identity/membership/metadata/alias and already-committed native state. Restored original authority MUST still complete genuine already-committed recovery, preserve state/native generations and remove only the original journal without owned staging residue. Existing captured journal-leaf checks, error causes and durability semantics MUST remain. No new budget, content/durability policy, full snapshot, whole transaction rollback or atomic uncooperative-writer exclusion is established.

#### Scenario: Journal cleanup refuses detached original parent

- **WHEN** real Apply or restart Recover substitutes the admitted named journal parent with a foreign real directory or external alias immediately before cleanup
- **THEN** cleanup MUST typed-refuse preserving both metadata-directory generations and committed native state, and restoring original authority MUST genuinely complete already-committed journal recovery without altering foreign metadata

## ADDED Requirements

### Requirement: SDK quarantine restoration retains observed namespace authority

SDK MUST capture no-follow descriptor-relative identity of the actual quarantined entry before verification and retain that observed device/inode/mode/size/link shape through mismatch restoration and quarantine cleanup. Link creation MUST account only for its own link-count change. Unknown/later generations MUST remain untouched while the original stale-prune conflict remains visible; they MUST NOT be restored as the observed entry. Existing legitimate regular/FIFO/changed-leaf restoration and actual stale pruning MUST remain functional. This is not an atomic uncooperative-writer compare-and-act policy or a change to removal reporting.

#### Scenario: SDK quarantine restoration preserves observed generation

- **WHEN** public SDK sync observes a genuine quarantined mismatch B after discovery of A and B is replaced at the restore boundary with independently authored foreign C
- **THEN** sync MUST retain the original stale conflict and preserve foreign C and retained A/B bytes/inodes/modes/mtimes, avoid unauthorized stale-name restoration, retain output members and only owned-stage cleanup, and restoring valid authority MUST permit real healthy pruning while foreign quarantine C stays unchanged

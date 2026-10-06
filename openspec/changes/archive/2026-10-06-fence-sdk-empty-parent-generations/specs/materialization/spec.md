## ADDED Requirements

### Requirement: SDK empty-parent cleanup retains admitted directory authority

SDK post-prune empty-parent cleanup MUST use captured output and parent descriptors, require the discovery-admitted parent/directory generations before directory-only removal, and propagate meaningful generation/inspection/close refusal instead of reporting successful cleanup. Unknown or replacement directories MUST be preserved. Nonempty directories MUST stop normal cleanup, and valid nested stale/empty-parent pruning MUST remain functional; the output root MUST never be removed.

#### Scenario: SDK empty-parent replacement preserves directory generation

- **WHEN** public SDK sync prunes a real nested stale payload and its admitted empty parent is replaced after initial inspection with an independently created foreign empty directory
- **THEN** SDK MUST generation-refuse without deleting/changing the foreign directory inode/mode/mtime or output membership, preserve the original retained now-empty parent, clean only owned staging, and restoring valid authority with a real stale payload MUST permit actual stale-file and empty-parent removal while foreign data stays unchanged

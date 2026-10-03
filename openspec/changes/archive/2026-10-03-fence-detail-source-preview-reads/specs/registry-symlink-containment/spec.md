## ADDED Requirements

### Requirement: Filesystem source previews use captured opened-file authority

Agent Marketplace filesystem detail-source capture and selected resource bundle previews MUST use descriptor-bound reads from their authorized containing directory or resource root. Opened payloads MUST be regular and singly linked, FIFO opens MUST not block, and opened state plus captured target generations MUST remain validated through the read. Final source symlinks MUST retain the existing no-follow refusal. Refused reads MUST not contribute payload bytes or mutate source/external generations. Existing per-source and aggregate capture limits, truncation markers, archive-backed previews and rendering behavior MUST remain supported without new processing budgets or whole-body buffering. No whole-source atomic snapshot is introduced.

#### Scenario: Detail source previews refuse multiply linked payloads

- **WHEN** an otherwise valid filesystem source preview becomes multiply linked or nonregular
- **THEN** source capture and selected bundle previews MUST refuse without importing payload bytes or blocking on a FIFO, preserve original file metadata/bytes, and retain the valid bounded/truncated preview controls

## ADDED Requirements

### Requirement: Producer support staging cleanup retains admitted directory authority

Agent Marketplace MUST clean producer support staging through its captured parent and MUST require the named directory to retain its admitted object identity before recursive deletion. A replaced or unavailable producer generation MUST refuse cleanup without removing replacement bytes or metadata. Cleanup refusal MUST propagate through materialize.write.error before cache-backed native link installation. A healthy attempt MUST remove only its own staging directory. This requirement does not make earlier cache/support publication transactional or establish producer creation/write admission.

#### Scenario: Support producer cleanup preserves replaced stage directory

- **WHEN** public native sync replaces the producer staging directory with a different regular directory after support publication staging
- **THEN** cleanup MUST preserve foreign bytes/inode/mode/mtime and retained original support, refuse native installation, and a healthy retry MUST install actual source-mode support and clean only its new stage while preserving the previous replacement

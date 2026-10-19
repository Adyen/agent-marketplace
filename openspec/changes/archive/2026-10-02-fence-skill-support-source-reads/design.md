## Context

Skill support copying used ReadDir/Stat/ReadFile paths and source equality probes. Existing contained relative source links and executable bits must remain useful. The installed cache is visible through native skill links, so source refusal must precede compiled writes and support pruning.

## Decisions

1. Capture source scope once, enumerate directories from captured handles in deterministic order, and use the shared repository-link destination/snapshot boundary for source leaf payloads. Read bytes and mode from one unchanged regular singly-linked generation.
2. Bound growth with captured size, without importing the old branch's arbitrary absolute source/tree limits.
3. Copy support into attempt-owned staging before touching compiled/support cache bytes. Existing declared-support validation remains in that pre-publication phase. Source refusal cleans only attempt-owned staging.
4. Preserve ordinary source content, contained relative aliases, executable modes, VCS/compiled exclusions and missing-optional-source compatibility. Existing real-sync support cache rebuilding remains unchanged; do not claim a new per-support inode/mtime repeat guarantee.
5. The support snapshot writer uses descriptor-derived equality/mode, does not path-chmod a leaf, and retains existing atomic-write behavior. It is not a claim that later cache parent publication or pruning boundaries are fully closed.

## Verification

The native Factory external-source witness failed before the reader guard. A subsequent installed-update witness failed because compiled cache bytes changed before source refusal; staging moved before that mutation. Valid contained alias/executable/source-preservation controls, existing direct no-op/mode and compiled/VCS exclusions, Linux/macOS, race, specs, CLI compatibility and costs remain required.

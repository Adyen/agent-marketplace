## 1. Regression coverage

- [x] Reproduce a newer acquisition lock with a stale consumer cache and add a failing TUI warning regression.
- [x] Bind missing-snapshot, pinned recovery, untrusted-cache, failed-sync, and visible-shortcut scenarios to behavior tests.

## 2. Implementation

- [x] Add TUI-only missing-snapshot warning and synchronization remediation while preserving unverified state.
- [x] Prioritize the existing sync shortcut and add a real-terminal recovery journey.
- [x] Update the existing TUI reference.

## 3. Verification and review

- [x] Run focused regressions, disk-I/O benchmark comparison, `make validate`, OpenSpec traceability, and TUI QA.
- [x] Review the intended diff using open-code-review-delegate and fix actionable findings.
- [x] Commit the implementation and verification evidence.
- [x] Run `openspec archive warn-missing-locked-git-snapshot --yes` as the final commit of this change.

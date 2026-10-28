## 1. Configuration readers

- [x] 1.1 Restore `main`'s rejection of unknown top-level Harness target IDs and remove the retain-and-warn path and its pipeline/telemetry filtering.

## 2. GUI

- [x] 2.1 Report unknown-target configuration through the existing invalid-configuration path: not editable, save refused, synchronization refused.
- [x] 2.2 Add a GUI host witness for refusal, unchanged files, repair, reload, save and sync.

## 3. Verification

- [x] 3.1 Run the affected Go packages, GUI checks and `make spec-check`.

## 1. Publication fence

- [x] Add annotated transactional regressions with valid initialization and retry controls.
- [x] Implement one transaction-held effective-head assertion for both supported stores.
- [x] Assert the head before any legacy metadata mutation.

## 2. Verification

- [x] Run affected store/consumer tests, race checks and before/after benchmarks.
- [x] Execute PostgreSQL checks or record the environment blocker explicitly.
- [x] Complete adversarial delegated review of the entire layer.
- [x] Archive with `openspec archive fence-legacy-assessment-publication --yes` as the final commit.

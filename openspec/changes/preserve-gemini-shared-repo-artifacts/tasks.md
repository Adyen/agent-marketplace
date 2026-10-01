## 1. Native-only scheduling

- [x] Add failing consumer regressions for command preservation and cleanup controls.
- [x] Limit extra shared-native machine work to native reconciliation.
- [x] Preserve existing scope refusals and incomplete-input fences.

## 2. Verification

- [ ] Run affected package and race checks, benchmarks, validation, and configuration E2E.
- [ ] Review the intended changes adversarially.
- [ ] Archive the change as the final commit and push #439 without changing #440.

## 1. Native-only scheduling

- [x] Add failing consumer regressions for command preservation and cleanup controls.
- [x] Limit extra shared-native machine work to native reconciliation.
- [x] Preserve existing scope refusals and incomplete-input fences.

## 2. Verification

- [x] Run affected package and race checks, benchmarks, validation, and configuration E2E.
- [x] Review the intended changes adversarially.
- [x] Archive the change for the final specification commit without changing #440.

## Review record

- Standard OCR-delegate review (GPT-6.1 Sol): `aff422c9..c57f6f95` plus the specification archive, 19/19 path/status entries reviewed, no skipped files or actionable findings. Its independent focused shared-scope regressions passed.
- Kimi OCR-delegate adversarial review: the same production diff through `c57f6f95` plus the specification archive, 9/9 initial review entries covered, no skipped files or critical/high/medium findings. It independently passed `make spec-check`.
- Both reviews agree that the fix is correct within its declared scope. Kimi also noted the pre-existing ordinary cross-scope coalescence gap and conservative retention trade-off, neither broadened into this fix.
- Kimi's optional cleanup-test observation prompted an explicit positive control proving native entries exist before removal. The parent reviewed this test-only strengthening and reran the focused and race checks before pushing; production bytes remain unchanged from `c57f6f95`.
- Passing tests and clean reviews do not prove the absence of defects.

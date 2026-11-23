## 1. Spec

- [x] 1.1 Add `gui-first-run` requirement "First-run stepper" with the numbered-steps scenario.

## 2. Implementation and witnesses

- [x] 2.1 Render the steps as an ordered list with `aria-current="step"` on the current step, and no separators.
- [x] 2.2 Add a Vitest witness bound to `gui-first-run/stepper-shows-numbered-steps-without-separators`.

## 3. Verify

- [x] 3.1 Run the full gui Vitest suite, vue-tsc, the first-run Playwright specs, and `make spec-check`.
- [x] 3.2 Capture after screenshots of the first-run dialog.

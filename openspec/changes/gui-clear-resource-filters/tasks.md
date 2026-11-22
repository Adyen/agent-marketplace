## 1. Spec

- [ ] 1.1 Add `gui-shell` requirement "Clear all resource filters" with the clear-filters scenario.

## 2. Implementation and witnesses

- [ ] 2.1 Add the Clear filters control to the active resource filter bar and route it, Diagnostics, and All resources through one clearing helper.
- [ ] 2.2 Add Vitest witnesses bound to `gui-shell/clear-filters-removes-every-active-filter`, including the single-filter case and focus on All resources after clearing.
- [ ] 2.3 Add a Playwright check for the bar-end placement and hover highlight, and save the two-filter bar screenshot.

## 3. Verify

- [ ] 3.1 Run the full GUI vitest suite, `vue-tsc --noEmit`, the touched Playwright test, and `make spec-check` from the repository root.

## 1. Spec

- [x] 1.1 Modify `gui-shell` requirement "Uninstall resource action" to sentence-case action names and dialog titles.

## 2. Implementation and witnesses

- [x] 2.1 Use sentence-case resource action names in the inspector and dialog titles.
- [x] 2.2 Show "User" for user scope in the inspector Scope value.
- [x] 2.3 Update Vitest and Playwright names and add a Scope witness.

## 3. Verify

- [x] 3.1 Run the full gui Vitest suite, vue-tsc, the resource-action Playwright specs, and `make spec-check`.
- [x] 3.2 Capture an after screenshot of the resource action dialog.

## 1. Spec

- [x] 1.1 Add `gui-shell` requirement "Sync and update feedback starts at the click" with its two scenarios.

## 2. Implementation and witnesses

- [x] 2.1 Track the pending Sync or Update request in the controller until the operation settles or fails to start.
- [x] 2.2 Show the progress toast and a busy indicator on the pressed toolbar button from that point.
- [x] 2.3 Add Vitest witnesses bound to `gui-shell/update-shows-feedback-before-the-operation-is-reported` and `gui-shell/failed-start-clears-the-feedback`.
- [x] 2.4 Re-run the held-update walk with a delayed action response and capture the busy button and toast during the hold.

## 3. Verify

- [x] 3.1 Run the affected GUI vitest files, the full GUI vitest suite, `vue-tsc --noEmit`, and `make spec-check` from the repository root.

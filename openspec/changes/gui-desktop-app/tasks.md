## 1. Desktop shell and workspace

- [x] 1.1 Add the desktop host using the Wails version selected by the `gui-local-api` transport spike and the frontend at the specified boundary; verify the CLI remains buildable with `CGO_ENABLED=0` and the GUI host is independently launchable.
- [x] 1.2 Implement user-scope-only workspace resolution, single-instance focus/handoff, explicit refresh, window-focus ordinary reload, and post-mutation reload; add annotated host/Playwright tests for each `gui-shell` scenario and verify repository/personal-local files are untouched and reloads retain the accepted view until final assessment.
- [x] 1.3 Add keyboard access, accessible names, and English-only controls; add annotated accessibility tests and verify every action can be completed with keyboard input.
- [x] 1.4 Implement the public GitHub Releases latest-stable check and GUI telemetry; add annotated tests for newer/no-newer releases, silent failures, and `gui` surface identity. Verify `gui.version_check: false` makes no network request and shows no notice.
- [x] 1.5 Verify no raw configuration editing action is exposed by the shell or its keyboard action map; bind an annotated test to that observable behavior.
- [x] 1.6 Implement the `session-interactions` migration, sync-and-quit, and quit confirmations; test affirmative, negative, and cancel paths, including closing the window while sync is pending.

## 2. Guided first run and configuration

- [x] 2.1 Show first run only when the user-personal root is absent; preselect detected Harness targets, allow changes, require a non-empty selection, and test the exact entry condition.
- [x] 2.2 Mirror interactive `agent-marketplace init`'s Community telemetry consent condition without a preselected GUI answer; test explicit organization-level skip, endpoint-only organization telemetry (still asks), explicit affirmative, and decline. Compare configuration bytes for identical Harness targets and consent answers, excluding sources.
- [x] 2.3 Add the optional source step after Harness and consent selection, skip it when Consumer organization configuration provides sources, and route edits through the `gui-configuration` mutation path; test cancellation before completion for zero writes.
- [x] 2.4 Compare equivalent GUI Harness/source edits byte-for-byte with existing CLI operations from identical starting roots. For source-only Git/directory additions and Marketplace endpoint removal, test that only the effective entry changes and included fragments remain byte-identical; verify no CLI commands are added.
- [x] 2.5 Show organization Harness targets and sources read-only; compare unmodeled includes, per-entry narrowing, and other fields with the equivalent CLI result or the specified effective-config delta, and identify them as “managed outside the GUI”.
- [x] 2.6 Repair shared configuration reading and explicit Harness target saving to retain already-present unknown top-level identifiers, warn about unsupported targets, reject newly introduced unsupported identifiers without writes, and retain strict validation of other fields and per-entry narrowing. Prove real TUI and GUI writes and initialization refusals, not unsaved form-state retention alone.
- [x] 2.7 Filter unsupported identifiers from shared resolution/materialization targets and basic telemetry Harness attributes; test a mixed supported/unsupported root and an unsupported-only root through target-dependent operations without unintended output.

## 3. Organization version-check policy

- [x] 3.1 Add CLI read support for the top-level `gui.version_check` setting in v4 and legacy config decoding; verify the organization value defaults to enabled when absent and an organization false value disables the GUI check.
- [x] 3.2 Ignore `gui.version_check` from user-personal, repository-committed, and personal-local layers and report a warning without rejecting those roots; add annotated tests for each layer.
- [x] 3.3 Add the `config-layering` scenario witnesses and the GUI no-network/no-notice witness; verify the setting is honored only from Consumer organization configuration.
- [x] 3.4 Add administrator documentation and release notes stating that the key may be deployed only after every managed client runs a release that reads it; verify no user-facing instruction recommends early deployment.

## 4. Surface parity and finalization

- [x] 4.1 Add GUI test annotations and GUI-surface witnesses for every `session-*` scenario; implement the `specaudit` parity check and verify a missing TUI or GUI witness fails with that surface named.
- [x] 4.2 Run host Go tests, Vitest, Playwright, `make spec-check`, and the affected `make validate`; require zero trace findings, no unbound scenarios, and no dangling annotations for this layer.
- [ ] 4.3 Run `openspec archive gui-desktop-app --yes` in this layer's pull request.

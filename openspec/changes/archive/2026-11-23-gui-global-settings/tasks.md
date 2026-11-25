## 1. Phase 2: backend

- [x] 1.1 Add the v4 user-personal settings editor in `internal/config/settings_edit.go`, reusing the telemetry editor's write path.
- [x] 1.2 Add the `settings` read payload in `internal/localapi/settings.go` and wire it into `configurationDTO`.
- [x] 1.3 Add the `save-settings` action with its locked and invalid refusals in `internal/localapi/settings.go`.
- [x] 1.4 Update `api/gui-local.yaml` and regenerate `gui/src/generated/api.d.ts`.
- [x] 1.5 Bind the scenarios with `// spec:` annotations in the Go tests.

## 2. Phase 3: Settings tabs UI

- [x] 2.1 Show backup, logging, update check, and materialization in the Settings General tab.
- [x] 2.2 Show the locked telemetry level and the organization defaults in the UI.
- [x] 2.3 Add GUI tests and Playwright coverage for the new Settings controls.

## 3. Verify

- [x] 3.1 Run the Go, gui, and Playwright checks and the spec check.
- [x] 3.2 Archive the change once Phase 3 is complete.

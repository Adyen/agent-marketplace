## 1. Transport spike and decision

- [x] 1.1 Spike whether the Wails v2 AssetServer handler can stream SSE on macOS WebKit and Linux WebKitGTK; record the result for both platforms before implementation.
- [x] 1.2 If v2 does not work on both platforms, spike Wails v3 SSE support on both. If v3 works, record an explicit adopt/decline decision in design.md before implementation proceeds; if neither works, select the authenticated loopback socket for production. Verify tests and development retain the loopback handler in every branch.

## 2. Contract and generated client

- [x] 2.1 Define and check in the OpenAPI contract for session snapshots, progress, notifications, and guarded actions; verify it describes every API operation and event without constraining the selected transport.
- [x] 2.2 Generate the frontend API client from OpenAPI and add a deterministic drift check; verify the check passes on generated files and fails after a controlled contract/client mismatch.

## 3. Local API implementation

- [x] 3.1 Implement the selected production transport and authenticated handler; verify every request and event stream requires a random per-launch credential that is not persisted, logged, or emitted in telemetry.
- [x] 3.2 When using a socket, enforce loopback binding, exact Host and GUI Origin validation, and no CORS; verify rejected hosts, origins, and missing or invalid credentials cause no side effects.
- [x] 3.3 Stream ordered snapshot, progress, and notification events with generation/sequence identity; verify clients can discard stale, duplicate, and out-of-order events.
- [x] 3.4 Route mutations through the shared guarded operation and return busy without side effects while another mutation is active; verify serialization against TUI operations.

## 4. Tests and finalization

- [x] 4.1 Add annotated Go handler/security/event tests and a Playwright E2E harness that runs the real core through the loopback handler started by a Go test helper; verify no public CLI flag is added.
- [x] 4.2 Run API generation/drift checks and `make spec-check`; require zero trace findings for this layer, with no unbound scenarios or dangling annotations, and verify the generated client is clean.
- [ ] 4.3 Run `openspec archive gui-local-api --yes` in this layer's pull request.

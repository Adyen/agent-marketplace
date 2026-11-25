## Context

See proposal.md for motivation. The API adapts the extracted session core while preserving the `session-startup`, `session-feedback`, and `session-interactions` contracts. Production access should remain in-process; Playwright and development require an independently testable handler.

## Goals / Non-Goals

**Goals:** Give the GUI a generated, checked API client; authenticate every request and event stream; preserve ordered session identity; and keep mutations single-writer.

**Non-Goals:** Expose a public CLI listener, make the API remotely reachable, bypass the core's guards, or add GUI presentation behavior.

## Decisions

- **Transport-neutral contract:** Keep the `gui-local-api` specification independent of the selected production transport. Its socket-specific restrictions apply only when a socket is exposed.
- **Spike first:** Verify whether Wails v2 AssetServer `Handler` supports streaming responses for SSE on both macOS WebKit and Linux WebKitGTK. If it does, serve `/api/*` in-process in production with no network socket. If v2 cannot stream on both platforms, check Wails v3. If v3 can stream on both, record an explicit decision to adopt or decline v3 in this change before implementation continues; if neither version works, use the loopback socket in production. Tests and development always use the loopback handler.
- **Transport decision (settled):** The Wails v2.14.0 `AssetServer.Handler` spike passed on macOS WebKit (production build) and on Linux WebKitGTK 4.1 under Xvfb: two independently authored SSE chunks reached a `fetch` + `getReader` consumer before the handler returned, while an ordinary fetch control stayed correct. Wails v3 was therefore not evaluated; the v3 condition was not triggered. Production serves `/api/*` in-process through `API.Handler()` with no network listener; the authenticated loopback socket (`API.ListenLoopback`) remains for development and tests only. In-process WebKit/WebKitGTK responses have no `http.Flusher`, so event writes use direct `Write` and call `Flush` only when the writer supports it; the loopback path uses real HTTP flushing. Recorded limitations: the Linux probe covered WebKitGTK 4.1 only and validated short two-chunk streams, not sustained load.
- **Fallback transport:** Bind only to the loopback interface at an ephemeral port. Send a random 256-bit per-launch token in a request header and require the exact loopback Host plus the GUI's own Origin. Reject unexpected hosts/origins before any operation; grant no CORS access. Do not persist, log, or emit the token in telemetry.
- **Window/test-harness identity:** Accept calls only from the GUI's own window and the test harness; share one middleware boundary for ordinary requests and streaming connections. Keep the test-only real-core handler in a Go test helper, with no public CLI flag.
- **Contract:** Check in OpenAPI, generate the frontend client with `openapi-typescript`, and mirror the Marketplace UI's `api:generate` / `api:drift` pattern so drift is a failing check rather than manual review.
- **Events:** Serialize snapshot, progress, and notification events from the session core into one ordered stream. Include the applicable session-startup/session-feedback generation and sequence identity; consumers discard stale or out-of-order events under the corresponding spec rules.
- **Mutations:** Route every mutating endpoint through the same guarded operation as the TUI. Return a busy response before side effects when an operation is active.

## Risks / Trade-offs

- [SSE behavior differs between WebKit and WebKitGTK] → Gate implementation on the v2 platform spike, then the v3 alternative/decision when needed, with the authenticated loopback handler as the final fallback.
- [Host or Origin validation is bypassed by alternate request forms] → Apply checks to every HTTP path, including event-stream setup, and test altered Host, Origin, missing/invalid token, and reconnect behavior without side effects.
- [Generated client drifts from the contract] → Make generation deterministic and include a no-diff drift command in CI.

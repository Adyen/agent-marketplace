## Implementation

- [x] Add failing tests bound to every `diagnostic-capture` scenario.
- [x] Add the bounded, redacting capture handler and attach it through `logging.Init`, including the terminal-session handler.
- [x] Hand the capture to diagnostic telemetry, with trace and span correlation and a discard path when capture is inactive.
- [x] Add `logs` and `logs_dropped` to the review payload.
- [x] Export the records via `otlploghttp` through the capture transport and replay `/v1/logs` with the other signals.
- [x] Document diagnostic logs in `docs/telemetry.md`.
- [x] Verify scoped tests, `make validate`, and `make spec-check`.
- [ ] Adversarially review every changed entry.

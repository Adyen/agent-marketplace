## Why

A `--diagnostic` run exports metrics and traces, but its operational log records stay in the local log file. Whoever investigates a shared trace cannot see what the CLI logged while the spans ran, and Grafana's trace-to-logs lookup (`spanId:<id> && traceId:<id>`) returns nothing because no CLI log record carries a trace or span ID.

## What Changes

- While diagnostic capture is active, the CLI also captures every log record of the invocation at debug level, regardless of the configured log level. The log file and stderr keep their configured levels.
- Each captured record carries the invocation's trace ID and a span ID: the span active in the record's context, or the root command span when there is none.
- Captured records are bounded, URL-credential redacted like spans and events, and listed in the review payload under `logs`.
- On consent, the CLI exports the records as OTLP logs to the primary endpoint's `/v1/logs`, next to metrics and traces. A rejected logs request fails the diagnostic export like a rejected metrics or traces request.
- Runs without diagnostic capture never send log records.

## Capabilities

### New Capabilities

- `diagnostic-capture`: per-invocation diagnostic log capture, review, and export.

## Impact

- New dependencies: `go.opentelemetry.io/otel/log`, `go.opentelemetry.io/otel/sdk/log`, `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp`.
- `internal/logging` gains a way to attach the capture handler to the process logger, including during TUI terminal sessions.
- The review payload gains `logs` and `logs_dropped`. Its `schema_version` stays `1`.
- The hosted collector must accept `/v1/logs` before this ships. Until it does, diagnostic exports fail and keep the payload. The collector change (a bounded logs schema filter and a logs pipeline) lives in k8s-multitenant and is tracked outside this repository.

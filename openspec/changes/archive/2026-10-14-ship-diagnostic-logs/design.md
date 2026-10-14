## Context

`--diagnostic` builds an in-memory metric reader and span exporter (`internal/telemetry/diagnostic.go`). At command end, `FinishDiagnostic` redacts the data, writes `payload-<trace-id>.json`, asks for consent, serializes the data with the official OTLP/HTTP exporters through a capture transport, and replays the captured requests to the primary endpoint.

Operational logs come from `log/slog`. `logging.Init` installs a process-wide default logger whose handlers are the JSON log file and, with `--debug`, a stderr mirror. `logging.BeginTerminalSession` replaces the default logger with the file-only handler while the TUI owns the screen. Root logging initializes before telemetry, and `command end` is logged after telemetry finishes.

The hosted collector (k8s-multitenant `agent-marketplace-web/production`) has no logs pipeline, and its trace backend receiver accepts no OTLP logs: `LogsService/Export` returns `Unimplemented` and `/v1/logs` returns 404.

## Goals / Non-Goals

**Goals:**

- Export the log records of a consented `--diagnostic` invocation, correlated by trace and span ID.
- Keep the existing review, consent, redaction, and failure semantics.

**Non-Goals:**

- Sending logs outside diagnostic capture.
- Changing log file contents, levels, or rotation.
- Choosing the hosted logs backend. The collector route is decided in k8s-multitenant; the client contract is the same for every route.

## Decisions

### Capture through an extra slog handler installed at logging init

When `--diagnostic` is set, `initRootLogging` passes a capture handler to `logging.Init`. Logging adds it beside the file and stderr handlers, and the managed terminal-session handler includes it too. The capture handler always enables debug, and the other handlers keep their own levels (the existing `multiHandler` already checks `Enabled` per handler). Capture therefore starts with the first `command start` record. Telemetry takes over the capture. When telemetry does not activate diagnostic capture (effective level not `detailed`, or telemetry unavailable), the captured records are discarded and nothing is written or sent.

Rejected: wrapping `slog.Default()` inside `telemetry.New`. That misses records logged before telemetry starts, and the TUI's terminal session would silently drop the wrapper.

### Bounds match the collector schema

The capture keeps at most 2,000 records, and at most 4 MiB of bounded message, attribute-key, and attribute-value bytes in total. It counts the rest in `logs_dropped`. Without the total budget, worst-case records (about 75 KiB each) could reach about 150 MiB, well past the collector's 20 MiB request limit, so every export would fail. A message longer than 8 KiB is truncated, with the suffix `…[truncated]` inside the limit. Group attributes become dotted keys, values are stringified, and values longer than 2 KiB are truncated the same way. Keys outside `^[a-z][a-z0-9_.]{0,63}$` are dropped, and a record keeps at most 32 attributes in emission order. These limits mirror the collector's logs filter, so a client record is never dropped server-side for size.

### Correlation uses the root span as fallback

Every record gets the root command span's trace ID. Its span ID is the context span when the record's context carries a valid span from the diagnostic tracer, and the root span otherwise. Most call sites log without a context, and the root-span fallback keeps them reachable through a span-scoped lookup. Records logged before the root span starts get the root span's IDs when telemetry takes over the capture.

### Export with the official OTLP logs exporter through the capture transport

At export, the CLI builds an `sdk/log` `LoggerProvider` with the redacted resource, a simple processor, and an `otlploghttp` exporter whose HTTP client is the existing capture transport. It then emits each captured record from instrumentation scope `agent_marketplace` (no version, no attributes) and flushes. The transport captures `/v1/logs` next to `/v1/metrics` and `/v1/traces`, and all captured requests are replayed with the existing send client, headers, CA, and 5-second timeout. `buildSignalEndpoint` gains a logs signal that is not a queue signal. Log records are never queued.

Rejected: building OTLP protobufs by hand. The other signals already rely on the official exporters for wire compatibility.

### Redaction and review payload

The message and every string attribute value go through `redact.URL` before the payload is written. The payload adds `logs` entries (`timestamp`, `severity`, `message`, `trace_id`, `span_id`, `attributes`) and `logs_dropped`. The exported records are built from the same redacted values, so the reviewed file and the export match.

## Risks / Trade-offs

- [Debug records may contain filesystem paths and error text] → Diagnostic capture already requires consent and documents this. The review file lists every record before export.
- [Collector not yet accepting `/v1/logs`] → Export fails and keeps the payload until the collector change deploys. Roll out the collector first.
- [Debug-level formatting cost] → Bounded to one invocation and 2,000 records.

## Migration Plan

1. Deploy the collector logs pipeline and its bounded filter.
2. Verify that an empty `/v1/logs` request returns 2xx at the hosted endpoint.
3. Release the CLI.

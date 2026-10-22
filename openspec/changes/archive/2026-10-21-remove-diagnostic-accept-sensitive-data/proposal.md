## Why

The root flag `--diagnostic-accept-sensitive-data` let a script or CI job send a diagnostic payload to the telemetry endpoint without anyone reviewing it. Diagnostic payloads can contain sensitive information, so consent should come from a person at a terminal who has seen the review notice.

## What Changes

- **BREAKING**: Remove the persistent root flag `--diagnostic-accept-sensitive-data`. Invocations that pass it fail with Cobra's unknown-flag error.
- Diagnostic export asks for y/N consent on a terminal, as before. Without a terminal it always keeps the review payload local and explains that export needs an interactive terminal.
- `telemetry preview --level diagnostic` asks for consent on a terminal and refuses without one.

## Capabilities

### New Capabilities

### Modified Capabilities
- `telemetry`: diagnostic export and diagnostic preview accept only interactive consent.

## Impact

Root flag registration and diagnostic completion in `cmd/agent-marketplace`, `Telemetry.FinishDiagnostic` in `internal/telemetry`, command, package, and E2E telemetry tests, and the telemetry and CLI reference pages under `site/docs`.

# How to configure telemetry

Enable, disable, and inspect telemetry for an unmanaged or organization-managed installation.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- An OTLP/HTTP endpoint for a personal installation, or an organization policy that sets the level.

## 1. Check the current level

```bash
agent-marketplace telemetry status
```

Output:

```text
Telemetry level: off
Installation ID: <id>
```

## 2. Enable basic telemetry

```bash
agent-marketplace telemetry enable --level basic
```

## 3. Enable detailed telemetry with an endpoint

```bash
agent-marketplace telemetry enable \
  --level detailed \
  --endpoint https://otel.example.com/v1
```

## 4. Disable telemetry

```bash
agent-marketplace telemetry disable
```

## 5. Configure through environment variables

For a single invocation without changing user settings:

```bash
AGENT_MARKETPLACE_TELEMETRY_LEVEL=basic \
AGENT_MARKETPLACE_TELEMETRY_ENDPOINT=https://otel.example.com/v1 \
  agent-marketplace sync
```

## 6. Preview a diagnostic capture

```bash
agent-marketplace telemetry preview --level detailed
```

This writes a representative JSON payload locally without sending it.

## Boundaries

- Organization settings cannot be overridden by user config or environment variables.
- `AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING=true` sends the basic schema to the project endpoint in addition to the primary endpoint.
- Standard `OTEL_*` environment variables are ignored for policy resolution.

## Troubleshooting

- **`telemetry status` reports locked fields**: An organization policy controls those fields. Change the value in the organization configuration.
- **Payloads are not sent**: Check the endpoint and any `ca_file` configured. Use `--debug` to see telemetry setup logs.

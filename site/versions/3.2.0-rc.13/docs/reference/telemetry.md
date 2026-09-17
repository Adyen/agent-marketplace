# Telemetry reference

## Levels

| Level | Signals |
|---|---|
| `off` | No telemetry is collected or sent. |
| `basic` | Command counts and durations with bounded, non-identifying attributes. |
| `detailed` | Basic metrics, configuration inventory gauges, and lifecycle traces. |

`diagnostic` is not a persistent level. It is an overlay on `detailed` enabled by the `--diagnostic` flag.

## Configuration schema

```yaml
telemetry:
  level: basic
  endpoint: https://otel.example.com/v1
  community_reporting: false
  ca_file: /etc/example/otel-ca.pem
  headers:
    Authorization:
      env: AGENT_MARKETPLACE_TELEMETRY_AUTH_TOKEN
    X-Tenant:
      file: /etc/example/tenant
```

## Fields

| Field | Type | Eligible scope | Description |
|---|---|---|---|
| `level` | string | user, organization | One of `off`, `basic`, `detailed`. |
| `endpoint` | URL | user, organization | Primary OTLP/HTTP endpoint. |
| `community_reporting` | boolean | user, organization | Send the basic schema to the project endpoint. |
| `ca_file` | path | user, organization | PEM CA bundle for the primary endpoint. |
| `allow_insecure` | boolean | organization only | Allow plain HTTP for organization-configured private endpoints. Ignored in user or local configuration. |
| `headers` | mapping | user, organization | Header values sourced from `env` or `file`. |

## Commands

- `agent-marketplace telemetry status`: effective level, authority, installation ID, and pending records.
- `agent-marketplace telemetry enable --level <basic|detailed> [--endpoint <url>]`: enable at the user level.
- `agent-marketplace telemetry disable`: set user level to `off`.
- `agent-marketplace telemetry preview --level <basic|detailed|diagnostic>`: write a local payload without sending.
- `agent-marketplace telemetry reset-id`: generate a new installation ID and purge queued payloads.
- `agent-marketplace telemetry purge`: remove queued records and retained diagnostic payloads.

## What is not collected

- Command arguments
- Registry credentials or local paths
- Resource contents, descriptions, or tags
- File paths other than the local diagnostic payload path
- User names, hostnames, prompts, terminal contents, or search text

## Diagnostic path

`--diagnostic` payloads are written to `$XDG_STATE_HOME/agent-marketplace/diagnostic/` (default `~/.local/state/agent-marketplace/diagnostic/`). Review the file before sharing.

## Precedence

- Organization settings cannot be overridden by user configuration or environment variables.
- Environment variables `AGENT_MARKETPLACE_TELEMETRY_LEVEL`, `AGENT_MARKETPLACE_TELEMETRY_ENDPOINT`, and `AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING` override user settings when the organization does not lock them.
- Standard `OTEL_*` variables are ignored.

## Related documentation

- [How to configure telemetry](../how-to/configure-telemetry.md)
- [Telemetry and privacy](../explanation/telemetry-and-privacy.md)

# Environment variables reference

This page lists environment variables that influence `agent-marketplace` behavior.

## Logging

| Variable | Effect |
|---|---|
| `AGENT_MARKETPLACE_LOG_ENABLED` | Enable or disable logging (`true` or `false`). |
| `AGENT_MARKETPLACE_LOG_LEVEL` | Set the log level (`debug`, `info`, `warn`, `error`). |
| `AGENT_MARKETPLACE_LOG_FILE` | Override the log file path. |
| `AGENT_MARKETPLACE_LOG_MAX_SIZE_MB` | Rotate when the file exceeds this size. |
| `AGENT_MARKETPLACE_LOG_MAX_BACKUPS` | Number of rotated files to keep. |
| `AGENT_MARKETPLACE_LOG_MAX_AGE_DAYS` | Delete rotated files older than this. |
| `AGENT_MARKETPLACE_LOG_COMPRESS` | Compress rotated files (`true` or `false`). |
| `AGENT_MARKETPLACE_DEBUG` | Force `debug` level and mirror logs to standard error. |

## Telemetry

| Variable | Effect |
|---|---|
| `AGENT_MARKETPLACE_TELEMETRY_LEVEL` | Set the telemetry level (`off`, `basic`, `detailed`). |
| `AGENT_MARKETPLACE_TELEMETRY_ENDPOINT` | Set the OTLP/HTTP endpoint. |
| `AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING` | Send the basic schema to the project endpoint (`true` or `false`). |

## Runtime

| Variable | Effect |
|---|---|
| `AGENT_MARKETPLACE_NO_TUI` | A non-empty value disables the TUI and prints help on a bare invocation. |
| `AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT` | Override the per-root ownership lock timeout. |

## Precedence

Environment variables override configuration file values. Logging and telemetry settings that are locked by organization policy cannot be overridden by environment variables.

## Related documentation

- [Logging reference](./logging.md)
- [Telemetry reference](./telemetry.md)

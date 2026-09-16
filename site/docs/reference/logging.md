# Logging reference

## Strategy

- Defaults live in code (`internal/config/config.go`).
- A `logging:` block in `~/.agents/config.yaml` is opt-in and overrides defaults.
- Environment variables override the config file.
- The `--debug` flag forces `debug` level and mirrors logs to standard error.

Effective precedence:

```text
environment variable > ~/.agents/config.yaml logging block > code default
```

## Scope

Logging is read from the user personal configuration only. Project and local `logging:` blocks are ignored.

## Default settings

| Setting | Default |
|---|---|
| `enabled` | `true` |
| `level` | `info` |
| `file` | `~/.agent-marketplace/logs/agent-marketplace.log` |
| `max_size_mb` | `10` |
| `max_backups` | `5` |
| `max_age_days` | `30` |
| `compress` | `true` |

## Configuration schema

```yaml
logging:
  enabled: true
  level: info        # debug | info | warn | error
  file: ~/.agent-marketplace/logs/agent-marketplace.log
  max_size_mb: 10
  max_backups: 5
  max_age_days: 30
  compress: true
```

`file` supports `~` and `$VAR` expansion.

## Environment variables

| Variable | Effect |
|---|---|
| `AGENT_MARKETPLACE_LOG_ENABLED` | Enable or disable logging. |
| `AGENT_MARKETPLACE_LOG_LEVEL` | Set the log level. |
| `AGENT_MARKETPLACE_LOG_FILE` | Override the log file path. |
| `AGENT_MARKETPLACE_LOG_MAX_SIZE_MB` | Rotation size threshold. |
| `AGENT_MARKETPLACE_LOG_MAX_BACKUPS` | Rotated files to keep. |
| `AGENT_MARKETPLACE_LOG_MAX_AGE_DAYS` | Max age of rotated files. |
| `AGENT_MARKETPLACE_LOG_COMPRESS` | Compress rotated files. |
| `AGENT_MARKETPLACE_DEBUG` | Force debug level and mirror to standard error. |

## Failover

When the log file cannot be opened, the CLI falls back to standard error rather than failing the command.

## Related documentation

- [Environment variables reference](./environment-variables.md)
- [Diagnostics reference](./diagnostics.md)

# Logging reference

Every `agent-marketplace` command writes a log file you can read after the command finishes. Logging needs no setup. You only add configuration when you want a different level, file, or rotation policy.

## How settings are chosen

Each setting comes from the first of these that sets it:

```text
environment variable > logging block in ~/.agents/config.yaml > built-in default
```

The `--debug` flag (or `AGENT_MARKETPLACE_DEBUG`) goes further: it sets the level to `debug` and also prints log records to standard error.

## Which configuration file counts

Only your user-personal [configuration file](../glossary.md#configuration-file), `~/.agents/config.yaml`, controls logging, including any files it pulls in with `include`.

- The organization file may contain a `logging:` block, and the CLI accepts it, but the CLI sets up logging before it reads organization settings. An organization `logging:` block has no effect on the log file, level, or rotation.
- Repository and personal-local configs cannot contain `logging:`. A v4 file that has one fails validation with `v4.layer-field`.
- If the user config, or any file it includes, fails to load for any reason, the CLI logs the warning `user logging config ignored` and uses the built-in logging defaults. A valid `logging:` block is ignored too when another part of the file is invalid.

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
  level: info        # debug | info | warn (or warning) | error
  file: ~/.agent-marketplace/logs/agent-marketplace.log
  max_size_mb: 10
  max_backups: 5
  max_age_days: 30
  compress: true
```

| Field | Rules |
|---|---|
| `enabled` | `false` stops writing the log file. With `--debug`, records still go to standard error. |
| `level` | One of `debug`, `info`, `warn`, `warning`, or `error`. |
| `file` | Path to the log file. `$VAR` references and a leading `~` are expanded. |
| `max_size_mb` | Size in megabytes at which the file is rotated. Must be greater than 0. |
| `max_backups` | Number of rotated files to keep. `0` keeps all of them. |
| `max_age_days` | Days to keep rotated files. `0` keeps them regardless of age. |
| `compress` | Whether rotated files are compressed with gzip. |

## Environment variables

| Variable | Effect |
|---|---|
| `AGENT_MARKETPLACE_LOG_ENABLED` | Turns the log file on or off. |
| `AGENT_MARKETPLACE_LOG_LEVEL` | Sets the log level. An unrecognized value falls back to `info`. |
| `AGENT_MARKETPLACE_LOG_FILE` | Sets the log file path. |
| `AGENT_MARKETPLACE_LOG_MAX_SIZE_MB` | Sets the rotation size. Only values above 0 are used. |
| `AGENT_MARKETPLACE_LOG_MAX_BACKUPS` | Sets how many rotated files to keep. Negative values are ignored. |
| `AGENT_MARKETPLACE_LOG_MAX_AGE_DAYS` | Sets how long to keep rotated files. Negative values are ignored. |
| `AGENT_MARKETPLACE_LOG_COMPRESS` | Turns compression of rotated files on or off. |
| `AGENT_MARKETPLACE_DEBUG` | Same as `--debug`. |

Boolean variables accept `1`, `true`, `yes`, `on`, `0`, `false`, `no`, and `off`, in any letter case. The CLI ignores any other value and keeps the setting from the config file or the default. The [environment variables reference](./environment-variables.md) lists every other variable the CLI reads.

## What the log contains

The log file holds one JSON object per line. Every record includes `run_id` (random for each command run), `version`, `command`, `pid`, `go_os`, and `go_arch`, so you can pick out the records from one run. Each command writes a `command start` record when it begins and a `command end` record with `duration_ms` and `success` when it finishes. A failed command also records the error.

With `--debug`, the same records also appear on standard error as readable `key=value` text.

## When the log file cannot be written

If the CLI cannot create the log folder or open the file, it prints a message ending in `using stderr fallback` and sends log records to standard error for that run. The command itself keeps running.

## Logging while the TUI runs

While the [TUI](../glossary.md#tui) is on screen, no log records are printed to the terminal, even with `--debug`, because they would corrupt the display. Records still go to the log file. If the log file is turned off or unavailable, records from that session are discarded.

## Related documentation

- [Environment variables reference](./environment-variables.md)
- [Diagnostics reference](./diagnostics.md)

# Logging

`agent-marketplace` writes operational logs to a rotating file so you can
inspect a run after it finishes. Logging needs no configuration. The defaults
live in code, and the generated user config does not contain a `logging:`
block. Add one only when you want to change a default.

## Strategy

- The `Default*` constants in `internal/config/config.go` are the single source
  of truth for the logging runtime in `internal/logging`.
- When `~/.agents/config.yaml` is missing, `agent-marketplace sync` (without
  `--offline` or `--locked`) creates it with only the required global fields:
  `apiVersion: agent-marketplace/v2` and `harnesses: [factory, windsurf]`. It
  does not add a `logging:` block. `agent-marketplace init` writes a v4 user
  config, also without one. To change a default, add the block yourself.
- Environment variables override both the config file and the code defaults,
  which is useful for a one-off debugging run without editing any file.

Effective precedence for each setting:

```
environment variable  >  ~/.agents/config.yaml (logging:)  >  code default
```

The logging runtime reads its settings only from the user config
(`~/.agents/config.yaml`, including any fragments it `include`s). Repository
(`.agents/config.yaml`) and personal-local (`.agents/config.local.yaml`)
configs may not contain `logging:`. A v4 file fails with `v4.layer-field`, and
a v2/v3 file fails with a "forbidden or unknown top-level field" error. The
organization layer accepts a `logging:` block, but it has no effect on the log
file, level, or rotation, because the root command initializes logging from
the user config alone (`config.LoadUserLoggingConfig`).

If the user config's `logging:` block is invalid, for example
`max_size_mb: 0`, logging falls back to the code defaults and writes a
`user logging config ignored` warning. Commands that load the user config
report the same validation error.

## Defaults

| Setting        | Default                                            |
| -------------- | -------------------------------------------------- |
| `enabled`      | `true`                                             |
| `level`        | `info`                                             |
| `file`         | `~/.agent-marketplace/logs/agent-marketplace.log`  |
| `max_size_mb`  | `10`                                               |
| `max_backups`  | `5`                                                |
| `max_age_days` | `30`                                               |
| `compress`     | `true`                                             |

The log file holds one JSON object per line. Log files are runtime state, not
regenerable cache, so they live under `~/.agent-marketplace/logs/` instead of
`~/.cache`. The CLI creates the directory with mode `0755` and the file with
mode `0644`.

Every record carries `run_id` (random per invocation), `version`, `command`,
`pid`, `go_os`, and `go_arch`. Each command writes a `command start` record
with the resolved config paths and a `command end` record with `duration_ms`
and `success` (plus `error` when the command failed).

If the CLI cannot create the log directory or open the file for writing, it
prints `agent-marketplace: <error>; using stderr fallback` and writes
text-format records to stderr for that run. The command itself does not fail.

## Overriding via config

Add a `logging:` block to `~/.agents/config.yaml`. Every key is optional, and
an omitted key keeps its code default. The block is the same in v2, v3, and v4
user configs.

```yaml
apiVersion: agent-marketplace/v2
harnesses:
  - factory
logging:
  enabled: true
  level: info        # debug | info | warn | error
  file: ~/.agent-marketplace/logs/agent-marketplace.log
  max_size_mb: 10    # rotate when the file exceeds this size
  max_backups: 5     # number of rotated files to keep (0 = keep all)
  max_age_days: 30   # delete rotated files older than this (0 = no age limit)
  compress: true     # gzip rotated files
```

`level` accepts `debug`, `info`, `warn` (or `warning`), or `error`, in any
case. `max_size_mb` must be positive. `max_backups` and `max_age_days` accept
`0`, which disables that limit. The `file` value supports `~` and `$VAR`
expansion.

## Overriding via environment variables

| Variable                            | Effect                                              |
| ----------------------------------- | --------------------------------------------------- |
| `AGENT_MARKETPLACE_LOG_ENABLED`     | Enable/disable logging (`true`/`false`)             |
| `AGENT_MARKETPLACE_LOG_LEVEL`       | Log level (`debug`/`info`/`warn`/`error`)           |
| `AGENT_MARKETPLACE_LOG_FILE`        | Log file path                                       |
| `AGENT_MARKETPLACE_LOG_MAX_SIZE_MB` | Rotation size threshold                             |
| `AGENT_MARKETPLACE_LOG_MAX_BACKUPS` | Rotated files to keep                               |
| `AGENT_MARKETPLACE_LOG_MAX_AGE_DAYS`| Max age of rotated files                            |
| `AGENT_MARKETPLACE_LOG_COMPRESS`    | Compress rotated files (`true`/`false`)             |
| `AGENT_MARKETPLACE_DEBUG`           | Force debug level and mirror logs to stderr         |

Boolean variables accept `1`/`0`, `true`/`false`, `yes`/`no`, and `on`/`off`.
The CLI silently ignores a boolean or number it cannot parse, and a number
outside the accepted range, and keeps the config or default value. An
unrecognized `AGENT_MARKETPLACE_LOG_LEVEL` value selects `info`.

The `--debug` flag has the same effect as `AGENT_MARKETPLACE_DEBUG=true`: it
forces the `debug` level and mirrors text-format records to stderr for the
run. Debug mirroring still reaches stderr when file logging is disabled. While
the interactive TUI owns the screen, the CLI stops mirroring to stderr so log
lines cannot corrupt the display; records keep going to the log file.

## For contributors

When you add or change a feature, instrument it with the standard library
`slog` default logger (`slog.Info`, `slog.Debug`, and so on). The root command
configures the process-wide logger before any subcommand runs, so structured
logging works everywhere without extra wiring. Put any new default in the
`Default*` constants in `internal/config/config.go`, and do not add a
`logging:` block to the generated config skeleton.

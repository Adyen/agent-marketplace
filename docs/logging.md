# Logging

`agent-marketplace` writes operational logs to a rotating file so that runs
can be inspected after the fact. Logging works **out of the box with no
configuration**: the defaults live in code, and the generated config skeleton
deliberately does not contain a `logging:` block. You only add one when you
want to override a default.

## Strategy

- **Code defaults are the baseline.** The shared `Default*` constants in
  `internal/config/config.go` are the single source of truth for the logging
  runtime (`internal/logging`).
- **Config is opt-in override, not boilerplate.** A `~/.agents/config.yaml`
  created implicitly by an install/remove/exclude edit carries only
  `apiVersion: agent-marketplace/v3` plus the fields the operation writes —
  no `harnesses:` list (the organization fallback must survive implicit
  creation; pick targets explicitly via `migrate`, `init`, or the TUI harness
  modal) and no `logging:` block, so the file stays clean. Add a `logging:`
  block yourself to change a default.
- **Env vars win for one-off runs.** Environment variables override both the
  config file and the code defaults, which is convenient for debugging without
  editing any file.

Effective precedence for each setting:

```
environment variable  >  ~/.agents/config.yaml (logging:)  >  code default
```

Logging configuration is **user-level only**: it is read from the global
config (`~/.agents/config.yaml`). It is not read from project
(`.agents/config.yaml`) or local (`.agents/config.local.yaml`) configs.

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

Logs are JSON lines. Log files are runtime state (not regenerable cache), so
they live under `~/.agent-marketplace/logs/`, outside `~/.cache`. When the log
file cannot be opened, the CLI falls back to stderr rather than failing the
command.

## Overriding via config

Add a `logging:` block to `~/.agents/config.yaml`. Every key is optional;
omitted keys fall back to the code default above.

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

`level` accepts `debug`, `info`, `warn`, or `error`. The `file` value supports
`~` and `$VAR` expansion.

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

The `--debug` flag is equivalent to `AGENT_MARKETPLACE_DEBUG=true`: it forces
the `debug` level and mirrors readable log records to stderr for the run.

## For contributors

When you add or change a feature, instrument it with the standard library
`slog` default logger (`slog.Info`, `slog.Debug`, etc.). The root command
configures the process-wide logger before any subcommand runs, so structured
logging is available everywhere without extra wiring. Keep new defaults in the
`Default*` constants in `internal/config/config.go` so the runtime stays the
single source of truth, and never re-introduce a `logging:` block into the
generated config skeleton.

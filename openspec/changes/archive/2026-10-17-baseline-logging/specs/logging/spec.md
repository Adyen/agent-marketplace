## ADDED Requirements

### Requirement: Logging is configured independently of resource acquisition

Operational logging SHALL use the user-personal configuration, including its accepted include tree, without requiring an acquisition lock. Generated user configuration SHALL omit a `logging` block so logging uses defaults unless explicitly configured.

#### Scenario: User logging settings include fragments

- **WHEN** a valid user root includes a fragment declaring logging settings
- **THEN** those logging settings MUST be loaded

#### Scenario: Logging needs no acquisition lock

- **WHEN** valid user logging settings coexist with an unreadable acquisition lock
- **THEN** the logging settings MUST remain available without reading that lock

#### Scenario: New user configuration omits logging overrides

- **WHEN** the CLI creates its default user configuration skeleton
- **THEN** it MUST create a valid exact-v4 root without a logging block

### Requirement: Logging settings preserve defaults and override precedence

File logging SHALL default to enabled, level `info`, `~/.agent-marketplace/logs/agent-marketplace.log`, rotation at 10 MiB, 5 backups, 30 days, and compression enabled. Valid `AGENT_MARKETPLACE_LOG_*` environment settings SHALL override configured values, which override defaults. Zero backups or age SHALL disable that rotation limit; rotation size SHALL remain positive. Boolean environment settings SHALL accept case-insensitive `1/0`, `true/false`, `yes/no`, and `on/off`; invalid boolean or integer settings SHALL retain the configured or default value. Unknown log levels SHALL select `info`. Log paths SHALL expand a leading tilde and environment variables.

#### Scenario: Default and overridden logging settings

- **WHEN** logging settings are absent, configured, or overridden by valid or invalid environment values
- **THEN** resolution MUST preserve the documented defaults, precedence, zero limits, boolean forms, fallback behavior, and path expansion

### Requirement: Log records are structured and debug is explicit

File output SHALL be JSON lines with `run_id`, `version`, `command`, `pid`, `go_os`, and `go_arch`. Default output SHALL suppress debug records. `--debug` or `AGENT_MARKETPLACE_DEBUG` SHALL force debug level and mirror text records to standard error outside an active terminal session.

#### Scenario: Default JSON output carries invocation context

- **WHEN** a logger records an info message and a debug message with default settings
- **THEN** the file MUST contain only the info record with the invocation context fields

#### Scenario: Debug records reach file and standard error

- **WHEN** debug mode is enabled and a debug message is recorded
- **THEN** the message MUST reach both the JSON log file and the standard-error mirror

#### Scenario: Environment selects the log file and debug level

- **WHEN** the environment overrides the configured log path and enables debug
- **THEN** debug output MUST use the environment-selected file rather than the configured path

### Requirement: Logging cannot corrupt the interactive terminal

While the TUI owns the terminal, logging SHALL use the managed file without mirroring to the live terminal. If no managed file is available, records SHALL be discarded for that terminal session.

#### Scenario: Active terminal session suppresses stderr mirroring

- **WHEN** a terminal session records logs with debug mirroring enabled
- **THEN** records MUST remain in the log file and MUST NOT reach live standard error

#### Scenario: Terminal session without a file discards records

- **WHEN** file logging is disabled or unavailable during a terminal session
- **THEN** log records MUST NOT reach live standard error

### Requirement: An unavailable file falls back to standard error

Outside an active terminal session, failure to create the log directory or open the log file SHALL report the failure and preserve logging through standard error.

#### Scenario: Unwritable destination retains fallback output

- **WHEN** the configured log directory cannot be created
- **THEN** initialization MUST report the failure and subsequent records MUST remain visible on standard error

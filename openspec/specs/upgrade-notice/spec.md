# upgrade-notice Specification

## Purpose
Tells users of release binaries and `go install` builds, at most once a day and without affecting command output, that a newer GitHub release exists.

## Requirements

### Requirement: Newer releases are detected by build kind

The CLI SHALL compare its own version with the latest published, non-prerelease GitHub release of `Adyen/agent-marketplace`. A version of the form `vMAJOR.MINOR.PATCH` SHALL be outdated when the latest release tag is a strictly higher semantic version. A Go pseudo-version SHALL be outdated when the latest release was published strictly after the commit time encoded in the pseudo-version. Any other version, including `dev`, `git describe` output with a commit suffix, and versions ending in `-dirty`, SHALL never be reported as outdated and SHALL NOT trigger a release lookup.

#### Scenario: Release build behind the latest release

- **WHEN** the running version is `v4.1.3` and the latest release is `v4.1.4`
- **THEN** the CLI SHALL report that `v4.1.4` is available

#### Scenario: Release build at or ahead of the latest release

- **WHEN** the running version is equal to or higher than the latest release tag
- **THEN** the CLI SHALL NOT report a newer release

#### Scenario: Pseudo-version built before the latest release

- **WHEN** the running version is a pseudo-version whose commit time precedes the latest release's publication time
- **THEN** the CLI SHALL report that the latest release was published after this build
- **AND** the notice SHALL name the `go install github.com/Adyen/agent-marketplace/cmd/agent-marketplace@latest` command

#### Scenario: Pseudo-version built after the latest release

- **WHEN** the running version is a pseudo-version whose commit time is at or after the latest release's publication time
- **THEN** the CLI SHALL NOT report a newer release

#### Scenario: Development builds are never checked

- **WHEN** the running version is `dev`, a `git describe` version with a commit suffix, or a version ending in `-dirty`
- **THEN** the CLI SHALL NOT contact GitHub and SHALL NOT report a newer release

### Requirement: Release lookups are cached and bounded

The CLI SHALL record each lookup attempt, successful or failed, with its time in `update-check.json` under the Agent Marketplace state root, and SHALL NOT contact GitHub again until 24 hours after the recorded attempt. A notice SHALL be computed from the cached release so that a command never waits on a lookup to decide its notice. When the cache is due for refresh, the lookup SHALL run concurrently with the command, and the process SHALL wait at most one second after the command completes for it to finish. A lookup that times out, fails, is rate limited, or returns a malformed body SHALL produce no output and SHALL NOT change the command's output or exit status.

#### Scenario: Fresh cache avoids network access

- **WHEN** a lookup was recorded less than 24 hours ago
- **THEN** the CLI SHALL NOT contact GitHub

#### Scenario: Failed lookup is silent and backs off

- **WHEN** the release lookup fails, times out, or returns a malformed body
- **THEN** the command's standard output, standard error, and exit status SHALL be unchanged
- **AND** the CLI SHALL NOT contact GitHub again within 24 hours

#### Scenario: Slow lookup does not delay exit beyond the bound

- **WHEN** a due lookup has not completed when the command finishes
- **THEN** the process SHALL exit no later than one second after the command finishes

### Requirement: The CLI notice is a throttled terminal-only stderr line

When a newer release is known, the CLI SHALL write exactly one line beginning with `info:` to standard error after the command finishes, naming the newer release and how to upgrade, and SHALL record when it did so. It SHALL NOT write the notice again within 24 hours of the recorded time. It SHALL NOT write the notice to standard output, and SHALL NOT write it when standard error is not a terminal.

#### Scenario: Notice is written once per day on a terminal

- **WHEN** a newer release is known, standard error is a terminal, and no notice was shown in the last 24 hours
- **THEN** the CLI SHALL write one `info:` line to standard error after the command's own output
- **AND** a second command within 24 hours SHALL NOT write the notice

#### Scenario: Non-terminal standard error receives no notice

- **WHEN** standard error is not a terminal
- **THEN** the CLI SHALL NOT write the notice and SHALL NOT contact GitHub

### Requirement: The release check can be suppressed

The CLI SHALL neither contact GitHub nor write a notice when `AGENT_MARKETPLACE_NO_UPDATE_CHECK` is set to a non-empty value, when `CI` is set to a non-empty value, or when the invoked command is `serve`, a shell completion request, or `--version`.

#### Scenario: Opt-out variable suppresses the check

- **WHEN** `AGENT_MARKETPLACE_NO_UPDATE_CHECK` is set to a non-empty value
- **THEN** the CLI SHALL NOT contact GitHub and SHALL NOT write a notice

#### Scenario: Continuous integration suppresses the check

- **WHEN** `CI` is set to a non-empty value
- **THEN** the CLI SHALL NOT contact GitHub and SHALL NOT write a notice

#### Scenario: Excluded commands suppress the check

- **WHEN** the invoked command is `serve`, a shell completion request, or `--version`
- **THEN** the CLI SHALL NOT contact GitHub and SHALL NOT write a notice

### Requirement: The TUI shows the notice as a bounded notification

When the TUI starts and a newer release is known under the same rules and throttle as the CLI notice, it SHALL show the notice once as a bounded notification. The lookup SHALL NOT delay the first rendered frame, and the opt-out and `CI` suppression SHALL apply.

#### Scenario: TUI shows an available release once

- **WHEN** the TUI starts and a newer release is known and no notice was shown in the last 24 hours
- **THEN** the TUI SHALL show one bounded notification naming the newer release
- **AND** the first frame SHALL render before any lookup completes

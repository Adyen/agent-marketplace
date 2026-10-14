## Why

`go install github.com/Adyen/agent-marketplace/cmd/agent-marketplace@latest` builds a commit of the main branch and stamps a Go pseudo-version. Nothing tells those users, or users of a downloaded release, that a newer release exists, so installations drift onto old versions. The repository is about to become public, which makes the latest GitHub release readable without credentials.

## What Changes

- The CLI checks the latest GitHub release of `Adyen/agent-marketplace` at most once every 24 hours, caching the result under the state root.
- A release build is notified when the latest release has a higher semantic version. A pseudo-version build is notified when the latest release was published after the build's commit time. Development builds (`dev`, `git describe` output, dirty builds) are never checked.
- The notice is one informational line on standard error after the command finishes, shown at most once every 24 hours, and only when standard error is a terminal.
- The TUI shows the same notice once as a bounded notification without delaying startup.
- `AGENT_MARKETPLACE_NO_UPDATE_CHECK`, a set `CI` variable, `serve`, shell completion, and `--version` suppress the check entirely.
- Check failures (offline, rate limits, malformed responses) are silent and never change a command's output or exit status.

## Capabilities

### New Capabilities

- `upgrade-notice`: when and how the CLI and TUI tell users a newer release is available.

## Impact

Adds `golang.org/x/mod` for semantic version and pseudo-version parsing. Adds one unauthenticated HTTPS request to `api.github.com` per machine per day. No configuration-file surface.

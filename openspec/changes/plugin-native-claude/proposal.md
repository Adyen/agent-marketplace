## Why

Plugin support (issue #6, v5.0.0) can register plugins natively, but every
shipped harness catalog keeps native delivery `unverified` until an isolated
probe proves the harness loads the registration. Claude Code is the first
harness to turn native delivery on. The candidate Claude registration shape
from `plugin-native-registration` is not yet proven: Claude requires a
marketplace `owner` and loads local plugins only through sources relative to
the marketplace root. Repository-scope registration names a catalog under
the user's state root, an absolute machine-local path that does not belong in
the shared, committed `.claude/settings.json`.

## What Changes

- The harness validation suite gains the `claude-native-plugin` probe: the
  installed Claude CLI, offline and with an isolated home, must report a
  natively delivered fixture plugin enabled and its component available, at
  user scope and at repository scope, and absent before sync.
- The Agent Marketplace-owned Claude catalog becomes a marketplace file
  Claude accepts: it names an owner and lists each plugin with a source
  relative to the catalog root.
- Repository-scope Claude registration moves to `.claude/settings.local.json`;
  user scope stays in `~/.claude/settings.json`. An existing registration in
  `.claude/settings.json` from the earlier candidate shape moves in one sync.
- The shipped Claude catalog declares native delivery verified for
  `claude-plugin` packages with the `claude-native-plugin` probe, so `auto`
  delivers `claude-plugin` packages natively to Claude Code.
- The released-client gate witnesses that the pinned released reader still
  passes on committed state after native Claude writes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `plugin-delivery`: native Claude registration files per scope and the
  one-sync move from the earlier repository registration file.
- `plugin-policy-catalogs`: the shipped Claude catalog declares native
  delivery verified with its probe.

## Impact

- `pkg/harness-compilation`: the Claude registration member and the shipped
  Claude catalog.
- `internal/materialize`: the Claude native catalog layout and registration
  destination.
- `tools/harness-compat`, `e2e`: the Claude native plugin probe and the
  released-client witness.
- `docs/harness-validation/claude.md`: the probe description.
- Factory Droid and Codex native delivery, git-sourced native catalog
  entries, user documentation, and the delivery-resolution rules are
  unchanged.

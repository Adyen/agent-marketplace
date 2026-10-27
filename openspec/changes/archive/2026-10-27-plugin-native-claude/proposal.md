## Why

Plugin support (issue #6, v5.0.0) can register plugins natively, but every
shipped harness catalog keeps native delivery `unverified` until an isolated
probe proves the harness loads the registration. Claude Code was to be the
first harness to turn native delivery on, but Claude Code 2.1.311 does not
load a plugin registered only through settings in an isolated, headless
session, so its native delivery stays `unverified`. The candidate Claude registration shape
from `plugin-native-registration` is not yet proven: Claude requires a
marketplace `owner` and loads local plugins only through sources relative to
the marketplace root. Repository-scope registration names a catalog under
the user's state root, an absolute machine-local path that does not belong in
the shared, committed `.claude/settings.json`.

## What Changes

- The Agent Marketplace-owned Claude catalog becomes a marketplace file
  Claude accepts: it names an owner and lists each plugin with a source
  relative to the catalog root.
- Repository-scope Claude registration moves to `.claude/settings.local.json`;
  user scope stays in `~/.claude/settings.json`. An existing registration in
  `.claude/settings.json` from the earlier candidate shape moves in one sync.
- The shipped Claude catalog keeps native delivery `unverified`, so `auto`
  still decomposes `claude-plugin` packages for Claude Code; explicit
  `delivery: native` stays refused.
- The released-client gate witnesses that the pinned released reader still
  passes on committed state after native Claude writes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `plugin-delivery`: native Claude registration files per scope and the
  one-sync move from the earlier repository registration file.

## Impact

- `pkg/harness-compilation`: the Claude registration member.
- `internal/materialize`: the Claude native catalog layout and registration
  destination.
- `internal/resolver`: a native plugin member no longer displaces an
  independently selected resource of the same name.
- `e2e`: the released-client witness.
- Factory Droid and Codex native delivery, git-sourced native catalog
  entries, user documentation, and the delivery-resolution rules are
  unchanged.

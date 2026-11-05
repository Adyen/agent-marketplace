# Proposal

## Why

Organizations may deploy `/etc/agent-marketplace/config.yaml` without an acquisition lock on purpose, so online `sync` acquires organization Marketplace evidence transiently. In that deployment `sync --locked`, `sync --offline`, and `lock --check` refuse with `v4.marketplace-lock-required`, which is correct, but the hint tells users to ask their administrator to record resources in the organization lock. The administrator has nothing to fix, so users file packaging escalations and maintainers misread the error.

## What Changes

- When the organization configuration is valid and its acquisition lock is absent, the `v4.marketplace-lock-required` hint says that the organization deploys no acquisition lock, so `--locked`, `--offline`, and `lock --check` cannot verify organization resources, and names an online `agent-marketplace sync` or `agent-marketplace sync --check` as the command that does. It does not mention the administrator or editing organization files.
- The refusal itself, its code, and its exit status do not change: read-only modes still refuse without acquiring or writing anything.
- Organization locks that are present but malformed, stale, wrong-scope, untrusted, or mismatched keep the administrator hint.

## Capabilities

### New Capabilities

### Modified Capabilities

- `command-error-output`: organization lock guidance carves out the intentionally absent organization lock, which names the online command instead of the administrator.

## Release note

When an organization deploys no acquisition lock, `sync --locked`, `sync --offline`, and `lock --check` still refuse with `v4.marketplace-lock-required`, but the hint now names an online `agent-marketplace sync` or `sync --check` instead of asking an administrator to ship a lock. Malformed, wrong-scope, and untrusted organization locks now show the administrator hint instead of lockfile editing advice.

## Impact

- `internal/config` (marks the absent-lock refusal), `internal/remedy` (hint selection), command tests, and the diagnostics reference docs.
- No change to Marketplace or Git lock formats, to lock writing, or to which modes may acquire.

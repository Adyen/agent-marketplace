## Why

When the source is already configured and its acquisition lock in the target
layer pins a commit behind the remote, an install from the
install-from-source modal can never succeed. The preview reports the remote
tip, but the install keeps the locked commit, so the expected-commit check
refuses with "moved since preview". The modal then previews again, sees the
tip again, and refuses again. Nothing moved: the lock is pinned, and the
remedy is `update`, not another preview.

## What Changes

- An expected-commit install whose commit was kept from the target layer's
  existing lock, and differs from the expected commit, refuses with a
  dedicated "locked" error naming the scope, the locked commit, the expected
  commit, and the `update` command that advances the source. It no longer
  reports that the source moved.
- The modal handles that refusal by returning to the Review step with the
  picks and scope kept and the remediation shown. It does not preview again.
- Real moves (a freshly resolved commit differing from the preview) keep the
  existing "moved; preview again" behavior.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-installation`: distinguishes a lock-pinned source from a moved
  source when an explicit-source install carries an expected commit.
- `tui-interactions`: the install-from-source modal recovers from a
  lock-pinned refusal without re-previewing.

## Impact

- `internal/pipeline`: install expected-commit check, prepared-dependency
  commit lookup, new error type.
- `cmd/agent-marketplace`: prepared install records whether a commit was
  kept from the existing lock.
- `internal/tui`: install-from-source install-error handling.
- `tools/tui-qa`: install-from-source journey.

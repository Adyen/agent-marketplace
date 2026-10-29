## Why

A resource can be selected in more than one scope, and the resource toggle
already writes such selections. An explicit-source install refuses to do the
same when another scope already declares the source and that scope's lock
pins a different commit: each layer's locked snapshot gets its own catalog,
so the install finds two catalogs for the source and fails with "did not
resolve to exactly one catalog". This blocks the install-from-source modal
from installing into Local after a lock-pinned Project refusal.

## What Changes

- An explicit-source install resolves the source's catalog from the target
  scope's declaration, so another scope declaring the same URL, ref, and path
  at a different locked commit no longer makes it fail. The other scope's
  configuration and lock stay unchanged.
- The modal's lock-pinned remediation again offers choosing another scope.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-installation`: explicit-source installs into a scope bind to that
  scope's declaration of the source.
- `tui-interactions`: after a lock-pinned refusal, choosing another scope
  installs.

## Impact

- `internal/pipeline`: explicit-source catalog selection in install.
- `internal/tui`: lock-pinned remediation text.
- `tools/tui-qa`: install-from-source lock-pinned journey.

## Release note

`install --from` into one scope no longer fails with "did not resolve to exactly one catalog" when another scope already declares the same source locked at a different commit. The other scope's configuration and lock stay unchanged.
